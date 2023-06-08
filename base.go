package common

import (
	"github.com/lijianjunljj/gocommon/config"
	"github.com/lijianjunljj/gocommon/config/parser"
)

func InitConfig(configFile string) {
	yamlParser := parser.NewYamlParser(configFile)
	yamlParser.Parse()
	Config = config.NewConfig(yamlParser)
	Config.InitDbType().InitMysql().InitRedis()
}
