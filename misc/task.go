package misc

import (
	"fmt"
	"github.com/streadway/amqp"
	"time"
)

type Task struct {
}

func (that *Task) Retry(intvalTime int, retryTimes int, f func() error) {
	err := f()
	if err != nil {
		go func() {
			timer := time.NewTimer(time.Duration(intvalTime) * time.Second)
			select {
			case <-timer.C:

				fmt.Println("正在重试中......", retryTimes)
				if retryTimes <= 0 {
					timer.Stop()
				} else {
					err = f()

					if err == nil {
						timer.Stop()
					}
				}
				retryTimes--
			}
		}()
	}

}
func (that *Task) Nack(d amqp.Delivery, intvalTime int) {
	go func(d amqp.Delivery) {
		timer := time.NewTimer(time.Duration(intvalTime) * time.Second)
		select {
		case <-timer.C:
			_ = d.Nack(false, true)
		}
	}(d)
}
