package consumer

import (
	"encoding/json"
	"fmt"
	"github.com/blackbeans/redigo/redis"
	"log"
	_ "os"
	"strconv"
	"time"
)

// 用于向flume中作为sink 通过thrift客户端写入日志

type SinkServer struct {
	queues       map[string][]*redis.Pool
	flumeClients []*flumeClient
	isStop       bool
}

func NewSinkServer(option *Option) (server *SinkServer) {

	queues := make(map[string][]*redis.Pool, 0)

	//创建redis的消费连接
	for _, v := range option.queueHostPorts {

		pool := redis.NewPool(func() (conn redis.Conn, err error) {

			conn, err = redis.DialTimeout("tcp", v.Host+":"+strconv.Itoa(v.Port),
				time.Duration(v.Timeout)*time.Second,
				time.Duration(v.Timeout)*time.Second,
				time.Duration(v.Timeout)*time.Second)

			return
		}, v.Timeout*2, v.Maxconn)

		// if nil != err {
		// 	log.Printf("open redis %s:%d fail!  %s\n", v.Host, v.Port, err.Error())
		// 	os.Exit(-1)
		// }

		pools, ok := queues[v.QueueName]
		if !ok {
			pools = make([]*redis.Pool, 0)
			queues[v.QueueName] = pools
		}

		queues[v.QueueName] = append(pools, pool)

	}

	flumeClients := make([]*flumeClient, 0)
	//创建flume的client
	for _, v := range option.flumeAgents {
		client := newFlumeClient(v.Host, v.Port)
		client.connect()
		flumeClients = append(flumeClients, client)
	}

	sinkserver := &SinkServer{queues: queues, flumeClients: flumeClients}

	return sinkserver
}

//启动pop
func (self *SinkServer) Start() {
	self.isStop = false
	for k, v := range self.queues {
		for i, pool := range v {
			fmt.Println(strconv.Itoa(i))

			conn := pool.Get()
			go func(queuename string, conn redis.Conn) {
				for !self.isStop {
					reply, err := conn.Do("LPOP", queuename)
					if nil != err || nil == reply {
						log.Printf("LPOP|FAIL|%s|%s", reply, err)
						time.Sleep(100 * time.Millisecond)
						continue
					}

					resp := reply.([]byte)
					var cmd command
					err = json.Unmarshal(resp, &cmd)

					if nil != err {
						log.Println("command unmarshal fail ! %s | error:%s", resp, err.Error())
						continue
					}

					//
					momoid := cmd.Params["momoid"].(string)

					businessName := cmd.Params["businessName"].(string)

					action := cmd.Params["type"].(string)

					bodyContent := cmd.Params["body"]

					body, err := json.Marshal(bodyContent)

					if nil != err {
						log.Printf("marshal log body fail %s", err.Error())
						continue
					}

					//这里需要优化一下body,需要采用其他的方式定义Body格式，写入

					log.Printf("%s,%s,%s,%s", momoid, businessName, action, string(body))

					//启动处理任务
					go func(momoid, businessName, action string, body string) {
						client := self.getFlumeClient(businessName, action)
						//拼装头部信息
						header := make(map[string]string, 1)
						header["businessName"] = businessName
						header["type"] = action

						//拼Body
						flumeBody := fmt.Sprintf("%s\t%s\t%s\n", momoid, action, body)
						err := client.append(header, []byte(flumeBody))

						if nil != err {
							log.Printf("send 2 flume fail %s \t err:%s\n", body, err.Error())
						} else {
							log.Printf("send 2 flume succ %s\n", body)
						}

					}(momoid, businessName, action, string(body))

				}
			}(k, conn)
		}
	}
}

func (self *SinkServer) Stop() {
	self.isStop = true
}

func (self *SinkServer) getFlumeClient(businessName, action string) *flumeClient {

	return self.flumeClients[0]
}