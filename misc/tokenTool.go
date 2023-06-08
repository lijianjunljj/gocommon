package misc

import (
	"github.com/lijianjunljj/gocommon/utils"
	// "crypto/md5"
	"errors"
	"fmt"
	"github.com/gomodule/redigo/redis"
	"time"
)

// TokenService Token服务
type TokenTool struct {
	rs redis.Conn
}

// NewTokenService 实例化Token服务
func NewTokenTool() *TokenTool {
	return &TokenTool{
		rs: GetRedis(),
	}
}

// TokenCreate 实现创建Token
func (s *TokenTool) TokenCreate(user map[string]interface{}) (string, error) {
	timestamp := utils.TimeMilliUnix()
	// 计算过期时间
	expParse, err := time.ParseDuration(Config.GetString("redis", "tokenuser", "exp"))
	if err != nil {
		return "", err
	}
	// str:=
	// b := []byte(str)
	// s := fmt.Sprintf("%x", md5.Sum(b))
	strUser := utils.MapToStr(user)
	strUid, ok := user["ID"].(string)
	if !ok {
		return "", fmt.Errorf("颁发证书失败，用户ID不匹配")
	}
	// 用户登陆状态
	uid := "U:" + strUid
	// 计算Token
	token := "T:" + strUid + ":" +
		utils.Md5(strUser+fmt.Sprint(timestamp)+Config.GetString("redis", "tokenuser", "secret"))
	// 关闭Redis连接
	defer s.rs.Close()
	// 检查用户是否登陆
	isExist, err := redis.Bool(s.rs.Do("EXISTS", uid))
	if err != nil {
		return "", err
	}
	// 已登陆
	if isExist {
		// 读取TOEKN KEY
		mytoken, err := redis.String(s.rs.Do("GET", uid))
		if err != nil {
			return "", err
		}
		// 删除TOKEN
		err = s.TokenDelete(mytoken, true)
		if err != nil {
			fmt.Printf("删除Token错误：%s", err.Error())
			// return "", err
		}
		// 删除登陆状态
		_, err = s.rs.Do("DEl", uid)
		if err != nil {
			return "", err
		}
	}
	// 存储登陆状态
	_, err = s.rs.Do("SET", uid, token)
	if err != nil {
		return "", err
	}
	// 存储TOKEN
	_, err = s.rs.Do("SET", token, strUser, "EX", expParse.Seconds())
	if err != nil {
		return "", err
	}
	return token, nil

}

// TokenVerify 校验Token
func (s *TokenTool) TokenVerify(token string) (map[string]interface{}, error) {
	var user map[string]interface{}
	defer s.rs.Close()
	isExist, err := redis.Bool(s.rs.Do("EXISTS", token))
	if err != nil {
		return user, err
	}
	if !isExist {
		fmt.Printf("校验Token失败，传入TOKEN：%s\n", token)
		return user, errors.New("请先登录")
	}
	bytes, err := redis.Bytes(s.rs.Do("GET", token))
	if err != nil {
		return user, err
	}
	err = utils.JSONDecode(bytes, &user)
	if err != nil {
		return user, err
	}
	err = s.TokenUpdate(token)
	if err != nil {
		return user, err
	}
	return user, nil

}

// TokenDelete 删除Token
func (s *TokenTool) TokenDelete(token string, noclose bool) error {
	// 不关闭连接
	if !noclose {
		defer s.rs.Close()
	}
	isExist, err := redis.Bool(s.rs.Do("EXISTS", token))
	if err != nil {
		return err
	}
	if !isExist {
		return nil
	}
	_, err = s.rs.Do("DEl", token)
	if err != nil {
		return err
	}
	return nil
}

// TokenUpdate 更新Token失效时间
func (s *TokenTool) TokenUpdate(token string) error {
	expParse, err := time.ParseDuration(Config.GetString("redis", "tokenuser", "exp"))
	if err != nil {
		return err
	}
	_, err = s.rs.Do("EXPIRE", token, expParse.Seconds())
	if err != nil {
		return err
	}
	return nil

}
