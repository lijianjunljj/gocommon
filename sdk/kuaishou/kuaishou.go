package kuaishou

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// 快手开放平台API基础地址
	BaseURL = "https://open.kuaishou.com"
	// Code2SessionURL code2Session接口地址（小程序）
	Code2SessionURL = BaseURL + "/oauth2/mp/code2session"
	// GetAccessTokenURL 获取接口调用凭证access_token接口地址
	GetAccessTokenURL = BaseURL + "/oauth2/access_token"
)

// Client 快手开放平台客户端
type Client struct {
	AppID      string
	AppSecret  string
	HTTPClient *http.Client
}

// NewClient 创建快手客户端
func NewClient(appID, appSecret string) *Client {
	return &Client{
		AppID:     appID,
		AppSecret: appSecret,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Code2SessionResponse code2Session响应结构
type Code2SessionResponse struct {
	Result     int    `json:"result"`      // 结果码，1表示成功
	ErrorMsg   string `json:"error_msg"`   // 错误信息
	SessionKey string `json:"session_key"` // 会话密钥
	OpenID     string `json:"open_id"`     // 用户唯一标识
}

// Code2Session 通过js_code获取session_key和open_id
// js_code: login 接口返回的 code,有效期为10分钟
func (c *Client) Code2Session(jsCode string) (*Code2SessionResponse, error) {
	params := url.Values{}
	params.Set("app_id", c.AppID)
	params.Set("app_secret", c.AppSecret)
	params.Set("js_code", jsCode)

	// POST请求，参数通过URL query传递（根据官方文档示例）
	reqURL := Code2SessionURL + "?" + params.Encode()

	req, err := http.NewRequest("POST", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result Code2SessionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Result != 1 {
		return nil, fmt.Errorf("快手API错误: %s", result.ErrorMsg)
	}

	return &result, nil
}

// GetAccessTokenResponse getAccessToken响应结构
type GetAccessTokenResponse struct {
	Result      int    `json:"result"`       // 结果码，1表示成功，非1表示错误码
	ErrorMsg    string `json:"error_msg"`    // 错误信息
	AccessToken string `json:"access_token"` // 接口调用凭证，48小时内有效
	ExpiresIn   int64  `json:"expires_in"`   // token过期时间，单位：秒
	TokenType   string `json:"token_type"`   // token类型，固定为bearer
}

// GetAccessToken 获取接口调用凭证access_token
// 使用 OAuth2 的 client credentials 模式，获取小程序全局唯一后台接口调用凭据
// access_token 48小时内有效，未超出有效截止时间重新调用获取新的access_token，则新老token同时有效
func (c *Client) GetAccessToken(grantType string) (*GetAccessTokenResponse, error) {
	if grantType == "" {
		grantType = "client_credentials"
	}

	params := url.Values{}
	params.Set("app_id", c.AppID)
	params.Set("app_secret", c.AppSecret)
	params.Set("grant_type", grantType)

	// GET请求，参数通过URL query传递（根据官方文档示例）
	reqURL := GetAccessTokenURL + "?" + params.Encode()

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result GetAccessTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Result != 1 {
		// 错误码说明
		errorMsg := result.ErrorMsg
		if errorMsg == "" {
			switch result.Result {
			case 100200100:
				errorMsg = "参数有误，需要检查参数是否为空或有误"
			case 100200101:
				errorMsg = "验证出错，需要检查app_id和app_secret"
			default:
				errorMsg = fmt.Sprintf("未知错误码: %d", result.Result)
			}
		}
		return nil, fmt.Errorf("快手API错误: %s", errorMsg)
	}

	return &result, nil
}

// CallAPI 调用快手开放接口（通用方法）
func (c *Client) CallAPI(apiURL string, accessToken string, params map[string]string) (map[string]interface{}, error) {
	reqURL, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("解析URL失败: %w", err)
	}

	query := reqURL.Query()
	query.Set("app_id", c.AppID)
	if accessToken != "" {
		query.Set("access_token", accessToken)
	}
	for k, v := range params {
		query.Set(k, v)
	}
	reqURL.RawQuery = query.Encode()

	req, err := http.NewRequest("GET", reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if resultCode, ok := result["result"].(float64); ok {
		if int(resultCode) != 1 {
			errorMsg := ""
			if msg, ok := result["error_msg"].(string); ok {
				errorMsg = msg
			}
			return nil, fmt.Errorf("快手API错误: %s", errorMsg)
		}
	}

	return result, nil
}

// ValidateCode 验证code是否有效
func (c *Client) ValidateCode(code string) (bool, error) {
	_, err := c.Code2Session(code)
	if err != nil {
		return false, err
	}
	return true, nil
}

// DecryptUserData 解密用户数据
// sessionKey: 有效的sessionKey，通过 login code 置换
// encryptedData: 返回的加密数据（base64编码）
// iv: 返回的加密IV（base64编码）
// 返回解密的字符串数据（JSON格式）
func DecryptUserData(sessionKey, encryptedData, iv string) (string, error) {
	// Base64解码
	aesKey, err := base64.StdEncoding.DecodeString(sessionKey)
	if err != nil {
		return "", fmt.Errorf("解码sessionKey失败: %w", err)
	}

	ivBytes, err := base64.StdEncoding.DecodeString(iv)
	if err != nil {
		return "", fmt.Errorf("解码iv失败: %w", err)
	}

	cipherBytes, err := base64.StdEncoding.DecodeString(encryptedData)
	if err != nil {
		return "", fmt.Errorf("解码encryptedData失败: %w", err)
	}

	// 验证密钥和IV长度
	if len(aesKey) != 16 {
		return "", fmt.Errorf("aesKey长度必须为16字节，当前为%d", len(aesKey))
	}
	if len(ivBytes) != 16 {
		return "", fmt.Errorf("iv长度必须为16字节，当前为%d", len(ivBytes))
	}

	// AES-128-CBC解密
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("创建AES cipher失败: %w", err)
	}

	if len(cipherBytes) < aes.BlockSize {
		return "", fmt.Errorf("密文长度不足")
	}

	// CBC模式需要密文长度是块大小的倍数
	if len(cipherBytes)%aes.BlockSize != 0 {
		return "", fmt.Errorf("密文长度必须是块大小的倍数")
	}

	mode := cipher.NewCBCDecrypter(block, ivBytes)
	plainBytes := make([]byte, len(cipherBytes))
	mode.CryptBlocks(plainBytes, cipherBytes)

	// PKCS#5填充移除（实际上是PKCS#7，但Go标准库会自动处理）
	// 查找最后一个字节的值，这表示填充的字节数
	paddingLen := int(plainBytes[len(plainBytes)-1])
	if paddingLen > len(plainBytes) || paddingLen == 0 {
		return "", fmt.Errorf("无效的填充长度")
	}

	// 验证填充是否有效
	for i := len(plainBytes) - paddingLen; i < len(plainBytes); i++ {
		if plainBytes[i] != byte(paddingLen) {
			return "", fmt.Errorf("填充验证失败")
		}
	}

	// 移除填充
	plainBytes = plainBytes[:len(plainBytes)-paddingLen]

	return string(plainBytes), nil
}

// VerifySignature 验证数据签名
// rawData: 原始数据（JSON字符串）
// sessionKey: 会话密钥
// signature: 签名值
// 返回验证是否通过
func VerifySignature(rawData, sessionKey, signature string) bool {
	// sha1ToHex(rawData + sessionKey) == signature
	data := rawData + sessionKey
	hash := sha1.Sum([]byte(data))
	hexHash := hex.EncodeToString(hash[:])
	return hexHash == signature
}

// UserInfo 用户信息结构（解密后的数据）
type UserInfo struct {
	OpenID    string `json:"openId"`    // 用户唯一标识
	NickName  string `json:"nickName"`  // 用户昵称
	AvatarURL string `json:"avatarUrl"` // 用户头像URL
	Gender    int    `json:"gender"`    // 性别：0-未知，1-男，2-女
	City      string `json:"city"`      // 城市
	Province  string `json:"province"`  // 省份
	Country   string `json:"country"`   // 国家
	Language  string `json:"language"`  // 语言
}

// DecryptAndParseUserInfo 解密并解析用户信息
func DecryptAndParseUserInfo(sessionKey, encryptedData, iv string) (*UserInfo, error) {
	plainData, err := DecryptUserData(sessionKey, encryptedData, iv)
	if err != nil {
		return nil, err
	}

	var userInfo UserInfo
	if err := json.Unmarshal([]byte(plainData), &userInfo); err != nil {
		return nil, fmt.Errorf("解析用户信息失败: %w", err)
	}

	return &userInfo, nil
}
