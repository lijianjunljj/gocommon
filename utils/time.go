package utils

import (
	"github.com/lijianjunljj/gocommon/config"
	"time"
)

// DateTimeFormat 年月日时分秒时间格式
const DateTimeFormat = config.DateTimeFormat

// TimeUnix 获取当前的时间Unix时间戳
func TimeUnix() int64 {
	return time.Now().Unix()
}

// TimeMilliUnix 获取当前的时间毫秒级时间戳
func TimeMilliUnix() int64 {
	return time.Now().UnixNano() / 1000000
}

// TimeMilliUnixAdd 获取当前的时间毫秒级时间戳
func TimeMilliUnixAdd(d string) int64 {
	duration, _ := time.ParseDuration(d)
	return time.Now().Add(duration).UnixNano() / 1000000
}
func DurationToSeconds(d string) float64 {
	duration, _ := time.ParseDuration(d)
	return duration.Seconds()
}

// TimeNanoUnix 获取当前的时间纳秒级时间戳
func TimeNanoUnix() int64 {
	return time.Now().UnixNano()
}

// DateTime2MilliUnix 时间字符串转毫秒时间戳
func DateTime2MilliUnix(datetime string) int64 {
	dt, _ := time.ParseInLocation(datetime, datetime, time.Local)
	return dt.UnixNano() / 1000000
}

// MilliUnix2DateTime 毫秒时间戳转时间字符串
func MilliUnix2DateTime(millisecond int64) string {
	return time.Unix(millisecond, 0).Format(DateTimeFormat)
}

// GetCurrentTimestamp 获取当天的时间范围
// Time类型 2022-05-19 00:00:00 +0800 CST 2022-05-19 23:59:59 +0800 CST
func GetCurrentTimestamp() (beginTime, endTime time.Time) {
	t := time.Now()
	timeStr := t.Format("2006-01-02")
	beginTime, _ = time.ParseInLocation("2006-01-02", timeStr, time.Local)
	endTimeTmp := beginTime.Unix() + 86399
	endTime = time.Unix(endTimeTmp, 0)
	return beginTime, endTime
}

func TodayStartTimeUnix() int64 {
	now := time.Now()
	year, month, day := now.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
	return today.Unix()
}
func GetMondayStart() time.Time {
	now := time.Now().UTC()                                        // 获取当前时间并转换为UTC时区
	weekday := now.Weekday()                                       // 获取今天是星期几
	delta := int(time.Monday-weekday+7) % 7                        // 计算距离本周一还有多少天
	monday := now.AddDate(0, 0, delta*-1).Truncate(24 * time.Hour) // 将当前时间向前调整到本周一零点
	return monday
}

// 获取本周星期一到星期日的unix时间戳
func GetMondayAndSundayUnixTime() (int64, int64) {
	// 获取本周第一天（周一）
	t := time.Now()
	weekDay := int(t.Weekday())

	if weekDay == 0 {
		weekDay = 7
	}

	monday := t.AddDate(0, 0, -weekDay+1)
	// 获取周一的零点时间
	zeroTime := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, t.Location())
	//monday := t.AddDate(0, 0, -int(t.Weekday()))
	//mondayStartOfDay := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.Local)
	unixMonday := zeroTime.Unix()

	// 获取本周最后一天（周日）
	sunday := t.AddDate(0, 0, -weekDay+8)
	sundayStartOfDay := time.Date(sunday.Year(), sunday.Month(), sunday.Day(), 0, 0, 0, 0, time.Local)
	unixSunday := sundayStartOfDay.Unix()

	return unixMonday, unixSunday
}

func getSecondsPassedToday() int64 {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return now.Unix() - todayStart.Unix()
}

// 获取上周一到上周日的时间范围
func getLastWeekUnixTimes() (int64, int64) {
	now := time.Now().In(time.FixedZone("Beijing Time", 8*3600))
	lastMonday := now.AddDate(0, 0, -int(now.Weekday())+1-7)
	lastSunday := lastMonday.AddDate(0, 0, 6)
	t := getSecondsPassedToday()
	return lastMonday.Unix() - t, lastSunday.Unix() - t
}
