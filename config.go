package main

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
)

type SpecificConf struct {
	Major uint8
	Minor uint32
	Redir string
}

type Config struct {
	Server struct {
		Bind string
	}
	Proxy struct {
		ConnectTimeout uint32
		Default        string
		Specific       []SpecificConf
	}
}

func NewConfig(configFile string) (*Config, error) {
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	var conf Config
	err = yaml.Unmarshal(data, &conf)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	return &conf, nil
}
