package main

import (
	"encoding/json"
	"io/ioutil"
)

type Config struct {
	KafkaAddr      string
	KafkaTopic     string
	ListenUDP      string
	ListenTCP      string
	SyslogNodeName string
	HttpAddr       string
}
type JsonStruct struct {
}

func NewJsonStruct() *JsonStruct {
	return &JsonStruct{}
}

func (jst *JsonStruct) Load(filename string, v interface{}) {
	//ReadFile函数会读取文件的全部内容，并将结果以[]byte类型返回
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return
	}

	//读取的数据为json格式，需要进行解码
	err = json.Unmarshal(data, v)
	if err != nil {
		return
	}
}
func (jst *JsonStruct) Writeconffile(filename string, v interface{}) {
	data, err := json.Marshal(v)
	if err == nil {
		err1 := ioutil.WriteFile(filename, data, 0777)
		if err1 != nil {
			return
		}
	} else {
		return
	}
}
