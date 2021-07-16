package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"net/http"
	_ "net/http/pprof" // 引入pprof,调用init方法

	"github.com/Shopify/sarama"
	"github.com/satori/go.uuid"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/wonderivan/logger"
	"gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/mcuadros/go-syslog.v2/format"
)

type SyslogMsg struct {
	RecvTime         string
	UUID             string
	SyslogServerAddr string
	SyslogNodeName   string
	Msgbody          map[string]interface{}
}

var conf Config                      //
var Receivedcount int64              //缓存
var WriteToKafkaCount int64          //
var channelRecv chan format.LogParts // 接收缓存通道
var StartTime string                 //启动时间
func main() {

	StartTime = time.Now().Format("20060102 15:04:05")
	os.MkdirAll("./logs", 0666)
	logger.SetLogger("./log.json")

	if IsExist("./conf.json") {
		JsonParse := NewJsonStruct()
		JsonParse.Load("./conf.json", &conf)
	} else {
		logger.Error("配置文件不存在")
		return
	}
	if conf.ListenTCP == "" {
		conf.ListenTCP = "0.0.0.0:514"
	}
	if conf.ListenUDP == "" {
		conf.ListenUDP = "0.0.0.0:514"
	}
	if conf.SyslogNodeName == "" {
		conf.SyslogNodeName = "SyslogServer"
	}
	if conf.KafkaTopic == "" {
		conf.KafkaTopic = "syslog"
	}
	if conf.KafkaAddr == "" {
		conf.KafkaAddr = "127.0.0.1:9092"
	}
	if conf.HttpAddr == "" {
		conf.HttpAddr = "127.0.0.1:80"
	}
	if conf.Debug == "" {
		conf.Debug = "false"
	}
	//
	if conf.Debug == "true" {
		// 生产环境应仅在本地监听pprof
		go func() {
			ip := "0.0.0.0:9527"
			if err := http.ListenAndServe(ip, nil); err != nil {
				fmt.Println("开启pprof失败", ip, err)
			}
		}()
	}
	//
	channelRecv = make(syslog.LogPartsChannel, 100000) // 接收日志通道

	//启动syslogserver
	handler := syslog.NewChannelHandler(channelRecv)
	server := syslog.NewServer()
	server.SetFormat(syslog.Automatic)
	server.SetHandler(handler)
	err := server.ListenUDP(conf.ListenUDP)
	if err != nil {
		logger.Error(err)
		return
	}
	err = server.ListenTCP(conf.ListenTCP)
	if err != nil {
		logger.Error(err)
		return
	}
	err = server.Boot()
	if err != nil {
		logger.Error(err)
		return
	}

	fmt.Println("Start UDP Server ...", conf.ListenUDP)
	fmt.Println("Start TCP Server ...", conf.ListenUDP)
	fmt.Println("Start Http Server ...", conf.HttpAddr)

	go dealSyslog()           // syslog分发处理处理
	go socketiorun()          // httpserver,启动socketio服务器
	go BroadcastMonitor()     // 广播状态
	go GetLatencyToSocketio() // 性能统计值
	go GetCPUMEMLatencyToSocketio()
	server.Wait()
	for {
	}
}

//日志处理函数
func dealSyslog() {
	for {
		config := sarama.NewConfig()
		config.Producer.RequiredAcks = sarama.WaitForLocal        // 发送完数据需要leader和follow都确认
		config.Producer.Partitioner = sarama.NewRandomPartitioner // 新选出一个partition
		config.Producer.Return.Successes = false                  // 成功交付的消息将在success channel返回
		config.Producer.Return.Errors = false

		kafkaaddr := strings.Split(conf.KafkaAddr, ",")
		kafkaclient, err := sarama.NewAsyncProducer(kafkaaddr, config)
		if err != nil {
			logger.Error("producer closed, err:", err)
			continue
		}
		defer kafkaclient.Close()
		go func(p sarama.AsyncProducer) {
			errors := p.Errors()
			success := p.Successes()
			for {
				select {
				case rc := <-errors: //出现了错误
					fmt.Println("err")
					if rc != nil {
						fmt.Println("send kafka data error")
					}
				case res := <-success:
					data, _ := res.Value.Encode()
					fmt.Printf("发送成功，value=%s \n", string(data))
				}
			}
		}(kafkaclient)
		LocalIPAddr := getLocalIP() //本地地址
		for {
			logParts, ok := <-channelRecv
			if ok {
				Receivedcount++
				var t SyslogMsg
				t.RecvTime = time.Now().Format("20060102150405")
				t.UUID = uuid.Must(uuid.NewV4()).String()
				t.SyslogServerAddr = LocalIPAddr
				t.SyslogNodeName = conf.SyslogNodeName
				t.Msgbody = logParts
				b, err := json.Marshal(t)
				if err != nil {
					logger.Error(err)
					continue
				}
				//保存到kafka
				msg := &sarama.ProducerMessage{
					Topic: conf.KafkaTopic,
					Value: sarama.ByteEncoder(b),
				}
				kafkaclient.Input() <- msg
				msg = nil
				WriteToKafkaCount++
			}
		}
	}
}
func IsExist(f string) bool {
	_, err := os.Stat(f)
	return err == nil || os.IsExist(err)
}

//获取本机IP地址
func getLocalIP() string {
	re := ""
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logger.Error(err)
		return ""
	}
	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				if re == "" {
					re = ipnet.IP.String()
				} else {
					re = re + "," + ipnet.IP.String()
				}
			}
		}
	}
	return re
}

type Latencys struct {
	ReceivedLatency        int64
	MaxReceivedLatency     int64
	MaxReceivedTime        string
	WriteToKafkaLatency    int64
	MaxWriteToKafkaLatency int64
	MaxWriteToKafkaTime    string
	Time                   string
	StartTime              string
}

//获取统计值
func GetLatencyToSocketio() {
	var t Latencys
	for {
		r1 := Receivedcount
		k1 := WriteToKafkaCount
		time.Sleep(1 * time.Second)
		r2 := Receivedcount
		k2 := WriteToKafkaCount
		t.ReceivedLatency = r2 - r1
		t.WriteToKafkaLatency = k2 - k1
		t.Time = time.Now().Format("20060102 15:04:05")
		if t.ReceivedLatency > t.MaxReceivedLatency {
			t.MaxReceivedLatency = t.ReceivedLatency
			t.MaxReceivedTime = t.Time
		}
		if t.WriteToKafkaLatency > t.MaxWriteToKafkaLatency {
			t.MaxWriteToKafkaLatency = t.WriteToKafkaLatency
			t.MaxWriteToKafkaTime = t.Time
		}
		t.StartTime = StartTime
		if socketioServer != nil {
			socketioServer.BroadcastToRoom("/", "syslogroom", "syslogLatencys", t)
		}
	}
}

//获取CPU\Mem
func GetCPUMEMLatencyToSocketio() {
	type Latencys struct {
		CPU        float64
		MaxCPU     float64
		MaxCPUTime string
		MEM        float64
		MaxMEM     float64
		MaxMEMTime string
		Time       string
	}
	var t Latencys
	for {
		t.CPU = GetCpuPercent()
		t.MEM = GetMemPercent()
		t.Time = time.Now().Format("20060102 15:04:05")
		if t.CPU > t.MaxCPU {
			t.MaxCPU = t.CPU
			t.MaxCPUTime = t.Time
		}
		if t.MEM > t.MaxMEM {
			t.MaxMEM = t.MEM
			t.MaxMEMTime = t.Time
		}

		if socketioServer != nil {
			socketioServer.BroadcastToRoom("/", "syslogroom", "syslogCPUMEMLatencys", t)
		}
	}
}
func GetCpuPercent() float64 {
	percent, _ := cpu.Percent(time.Second, false)
	return percent[0]
}

func GetMemPercent() float64 {
	memInfo, _ := mem.VirtualMemory()
	return memInfo.UsedPercent
}
