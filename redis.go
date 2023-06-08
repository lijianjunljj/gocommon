package common

import (
	"fmt"
	"github.com/gomodule/redigo/redis"
	"github.com/lijianjunljj/gocommon/utils"
	"github.com/mna/redisc"
	"strings"
	"time"
)

// redisPool 缓存Redis连接池
var redisPool *redis.Pool
var redisCluster *redisc.Cluster

// 初始化
func RedisInit() {
	InitRedis()
}

// RedisConnect 建立Redis连接
func RedisConnect() (redis.Conn, error) {
	timeout := time.Duration(utils.StrToInt(Config.GetString("redis", "timeout")))
	client, err := redis.Dial("tcp",
		//Redis连接地址
		Config.GetString("redis", "addr"),
		//Redis连接密码
		redis.DialPassword(Config.GetString("redis", "password")),
		//写入数据库号
		redis.DialDatabase(utils.StrToInt(Config.GetString("redis", "dbauthuser"))),
		//连接超时时间
		redis.DialConnectTimeout(timeout*time.Second),
		//读取超时时间
		redis.DialReadTimeout(timeout*time.Second),
		//写入超时时间
		redis.DialWriteTimeout(timeout*time.Second))
	return client, err
}

// NewRedisPool 建立Redis连接池
func NewRedisPool() *redis.Pool {
	pool := &redis.Pool{
		//连接池最大连接数
		MaxIdle: utils.StrToInt(Config.GetString("redis", "maxidle")),
		//连接池最大空闲连接数
		MaxActive: utils.StrToInt(Config.GetString("redis", "maxactive")),
		//空闲连接超时时间
		IdleTimeout: time.Duration(utils.StrToInt(Config.GetString("redis", "maxidletimeout"))) * time.Second,
		Wait:        true,
		Dial: func() (redis.Conn, error) {
			return RedisConnect()
		},
	}

	return pool
}

// NewRedisClusterPool 创建Redis集群连接池
func NewRedisClusterPool(addr string, opts ...redis.DialOption) (*redis.Pool, error) {
	return &redis.Pool{
		MaxIdle:     utils.StrToInt(Config.GetString("redis", "maxidle")),
		MaxActive:   utils.StrToInt(Config.GetString("redis", "maxactive")),
		IdleTimeout: time.Duration(utils.StrToInt(Config.GetString("redis", "maxidletimeout"))) * time.Second,
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", addr, opts...)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}, nil
}

// NewRedisCluster //Redis集群
func NewRedisCluster() *redisc.Cluster {
	timeout := time.Duration(utils.StrToInt(Config.GetString("redis", "timeout")))
	cluster := redisc.Cluster{
		StartupNodes: strings.Split(Config.GetString("redis", "addr"), ","),
		DialOptions: []redis.DialOption{
			redis.DialPassword(Config.Redis.Password),
			redis.DialDatabase(utils.StrToInt(Config.Redis.DbauthUser)),
			redis.DialConnectTimeout(timeout * time.Second),
			redis.DialReadTimeout(timeout * time.Second),
			redis.DialWriteTimeout(timeout * time.Second),
		},
		CreatePool: NewRedisClusterPool,
	}
	if err := cluster.Refresh(); err != nil {
		// utils.Error(fmt.Sprintf("Redis Cluster Refresh failed: %v", err))
		fmt.Printf("Redis Cluster Refresh failed: %v", err)
		return nil
	}
	return &cluster

}

// InitRedis 初始化Redis连接
func InitRedis(isCluster ...bool) {

	if len(isCluster) == 0 || !isCluster[0] {
		redisPool = NewRedisPool()
		return
	}
	redisCluster = NewRedisCluster()
}

// Redis 获取Redis连接
func Redis() redis.Conn {
	addrs := strings.Split(Config.Redis.Addr, ",")
	if len(addrs) == 1 {

		if redisPool == nil {
			InitRedis()
		}
		//fmt.Println(redisPool.Stats())
		return redisPool.Get()
	}
	if redisCluster == nil {
		InitRedis(true)
	}
	return redisCluster.Get()

}

func UnLockUser(userId string, resLockKey string) error {
	rs := Redis()
	defer rs.Close()
	userId1, err := redis.String(rs.Do("GET", resLockKey+":"+userId))
	if err != nil {
		return err
	}
	if userId1 == userId {
		_, err = rs.Do("DEL", resLockKey)
		if err != nil {
			return err
		}
	}
	return nil
}

func LockUser(userId string, resLockKey string) bool {
	rs := Redis()
	defer rs.Close()
	res, _ := rs.Do("SET", resLockKey+":"+userId, userId, "EX", 10, "NX")
	//fmt.Println("res.err:", res, err)
	if res == "OK" {
		return true
	}
	return false
}
