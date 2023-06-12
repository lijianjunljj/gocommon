package notify

import (
	"github.com/lijianjunljj/gocommon/misc"
)

type TaskRequest struct {
	misc.Task
	queueName string
	args      map[string]interface{}
}

func NewTaskRequest(...interface{}) *TaskRequest {
	args := map[string]interface{}{"x-message-ttl": 10000}
	return &TaskRequest{
		queueName: QUEUE_NAME_NOTIFY_RESULT,
		args:      args,
	}
}
func (ai *TaskRequest) Produce(mqReq *NotifyResult) error {
	mq := misc.GetMQ()

	err := mq.Produce(ai.queueName, mqReq, ai.args)
	return err
}
