package utils

import (
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/go-zero/core/discov"
	"fmt"
)
func ZrpcConn(serviceName string,option ...interface{}) zrpc.Client  {
	var dsn string
	if len(option) > 0  {
		dsn = option[0].(string)
	}else{
		dsn = "127.0.0.1:2379"
	}
	fmt.Println("dsn:",dsn)
	conn := zrpc.MustNewClient(zrpc.RpcClientConf{
		Etcd: discov.EtcdConf{ // 通过 etcd 服务发现时，只需要给 Etcd 配置即可
			Hosts: []string{dsn},
			Key:   serviceName,
		},
		NonBlock: true,
	})
	return conn
}
