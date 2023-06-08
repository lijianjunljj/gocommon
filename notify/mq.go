package notify

const (
	QUEUE_NAME_NOTIFY_RESULT = "notify_result"
)

type NotifyResult struct {
	UserId   string      `json:"user_id"`
	TaskId   interface{} `json:"task_id"`
	DistUrl  string      `json:"dist_url"`
	DistText string      `json:"dist_text"`
}

type NotifyChatgptResult struct {
	UserId   string      `json:"user_id"`
	TaskId   interface{} `json:"task_id"`
	DistUrl  string      `json:"dist_url"`
	DistText string      `json:"dist_text"`
}
