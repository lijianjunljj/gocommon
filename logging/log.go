package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type FileMgr struct {
	files map[*os.File]struct{}
	lock  sync.Mutex
}

func (f *FileMgr) Init() {
	f.files = make(map[*os.File]struct{})
}
func (f *FileMgr) AddFile(file *os.File) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.files[file] = struct{}{}
}

func (f *FileMgr) Collect() {
	Debug("start connect....", len(f.files))
	f.lock.Lock()
	defer f.lock.Unlock()
	for k, _ := range f.files {
		err := k.Close()
		if err == nil {
			delete(f.files, k)
		}
	}
	Debug("end connect....", len(f.files))
}

type Level int

var (
	F       *os.File
	fileMgr FileMgr

	DefaultPrefix      = ""
	DefaultCallerDepth = 2

	logger      *log.Logger
	logPrefix   = ""
	levelFlags  = []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
	logFilePath = ""
)

const (
	DEBUG Level = iota
	INFO
	WARNING
	ERROR
	FATAL
)

func Init(conf *Config) {
	fileMgr.Init()
	LogSavePath = conf.SavePath
	LogSaveName = conf.SaveName
	LogFileExt = conf.FileExt
	TimeFormat = conf.Format
	if LogSavePath == "" {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	} else {
		logFilePath = getLogFileFullPath()
		F = openLogFile(logFilePath)
		logger = log.New(F, DefaultPrefix, log.LstdFlags)
		ImportStd()
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				updateLogFile()
			}
		}()
	}
}

func updateLogFile() {
	newLogFilePath := getLogFileFullPath()
	if logFilePath != newLogFilePath {
		newF := openLogFile(newLogFilePath)
		logger.SetOutput(newF)
		logger.SetPrefix(DefaultPrefix)
		logger.SetFlags(log.LstdFlags)
		if F != nil {
			fileMgr.AddFile(F)
			go func() {
				fileMgr.Collect()
			}()
		}
		F = newF
		ImportStd()
	}
}

func ImportStd() {
	log.SetOutput(F)
	os.Stdout = F
	os.Stderr = F
}

func Debug(v ...interface{}) {
	setPrefix(DEBUG)
	logger.Println(v...)
}

func Info(v ...interface{}) {
	setPrefix(INFO)
	logger.Println(v...)
}

func Warn(v ...interface{}) {
	setPrefix(WARNING)
	logger.Println(v...)
}

func Error(v ...interface{}) {
	setPrefix(ERROR)
	logger.Println(v...)
}

func Fatal(v ...interface{}) {
	setPrefix(FATAL)
	logger.Fatalln(v...)
}

func setPrefix(level Level) {
	_, file, line, ok := runtime.Caller(DefaultCallerDepth)
	if ok {
		logPrefix = fmt.Sprintf("[%s][%s:%d]", levelFlags[level], filepath.Base(file), line)
	} else {
		logPrefix = fmt.Sprintf("[%s]", levelFlags[level])
	}

	logger.SetPrefix(logPrefix)
}
