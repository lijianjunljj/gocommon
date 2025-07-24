package utils

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

// ResponseWrapper 响应结构体
type ResponseWrapper struct {
	StatusCode int
	Body       string
	Header     http.Header
}

// Get Get请求
func Get(url string, timeout int) ResponseWrapper {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return createRequestError(err)
	}

	return HttpRequest(req, timeout)
}

// PostParams Post表单请求
func PostParams(url string, params string, timeout int) ResponseWrapper {
	buf := bytes.NewBufferString(params)
	req, err := http.NewRequest("POST", url, buf)
	if err != nil {
		return createRequestError(err)
	}
	req.Header.Set("Content-type", "application/x-www-form-urlencoded")

	return HttpRequest(req, timeout)
}

// PostJSON Post JSON请求
func PostJSON(url string, body string, timeout int) ResponseWrapper {
	buf := bytes.NewBufferString(body)
	req, err := http.NewRequest("POST", url, buf)
	if err != nil {
		return createRequestError(err)
	}
	req.Header.Set("Content-type", "application/json")

	return HttpRequest(req, timeout)
}

func PostJSONByAuth(url string, body string, auth string, timeout int) ResponseWrapper {
	buf := bytes.NewBufferString(body)
	req, err := http.NewRequest("POST", url, buf)
	if err != nil {
		return createRequestError(err)
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("Authorization", auth)
	return HttpRequest(req, timeout)
}

func HttpRequest(req *http.Request, timeout int) ResponseWrapper {
	wrapper := ResponseWrapper{StatusCode: 0, Body: "", Header: make(http.Header)}
	client := &http.Client{}
	if timeout > 0 {
		client.Timeout = time.Duration(timeout) * time.Second
	}
	setRequestHeader(req)
	resp, err := client.Do(req)
	if err != nil {
		wrapper.Body = fmt.Sprintf("执行HTTP请求错误-%s", err.Error())
		return wrapper
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		wrapper.Body = fmt.Sprintf("读取HTTP请求返回值失败-%s", err.Error())
		return wrapper
	}
	wrapper.StatusCode = resp.StatusCode
	wrapper.Body = string(body)
	wrapper.Header = resp.Header

	return wrapper
}

func setRequestHeader(req *http.Request) {
	req.Header.Set("User-Agent", "golang/gocron")
}

func createRequestError(err error) ResponseWrapper {
	errorMessage := fmt.Sprintf("创建HTTP请求错误-%s", err.Error())
	return ResponseWrapper{0, errorMessage, make(http.Header)}
}

type HttpFile struct {
	FileName string
	FileBuff []byte
}

func createReqBody(params map[string]string, files []HttpFile) (string, io.Reader, error) {
	var err error

	buf := new(bytes.Buffer)
	bw := multipart.NewWriter(buf) // body writer
	for k, v := range params {
		p1w, err := bw.CreateFormField(k)
		if err != nil {
			return "", nil, err
		}
		p1w.Write([]byte(v))
	}

	for k, v := range files {
		f := bytes.NewReader(v.FileBuff)
		fw1, err := bw.CreateFormFile("file"+strconv.Itoa(k), v.FileName)
		if err != nil {
			return "", nil, err
		}
		io.Copy(fw1, f)
	}
	bw.Close()
	return bw.FormDataContentType(), buf, err
}

func PostUpload(url string, params map[string]string, files []HttpFile) error {
	// create body
	contType, reader, err := createReqBody(params, files)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, reader)

	// add headers
	req.Header.Add("Content-Type", contType)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("request send error:", err)
		return err
	}
	resp.Body.Close()
	return nil
}
