package misc

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lijianjunljj/gocommon/logging"
	"github.com/streadway/amqp"
	"strings"
	"sync"
)

var mqInstance *MQ

type MQ struct {
	con       *amqp.Connection
	dsn       string
	wg        *sync.WaitGroup
	channel   *amqp.Channel
	done      chan bool
	customers map[string]*RabbitMqCustomer
	//Name        string
	//args        map[string]interface{}
	//queue       *amqp.Queue
}

func (m *MQ) init() {
	m.customers = make(map[string]*RabbitMqCustomer)
	m.wg = new(sync.WaitGroup)
	m.dsn = m.getDSN()
	m.connect()
	m.wg.Add(1)
	go m.reconnect()
}

func (m *MQ) reconnect() {
	defer m.wg.Done()
	var gracefulValue *amqp.Error
	graceful := make(chan *amqp.Error)
	errs := m.channel.NotifyClose(graceful)
	for {
		select {
		case <-m.done:
			return
		case gracefulValue = <-graceful:
			if gracefulValue == nil {
				break
			}
			fmt.Println("Graceful close!", gracefulValue)
			graceful = make(chan *amqp.Error)

			err := m.connect()
			for err != nil {
				err = m.connect()
			}
			errs = m.channel.NotifyClose(graceful)
		case gracefulValue = <-errs:

			if gracefulValue == nil {
				break
			}
			fmt.Println("Graceful normal!", gracefulValue)
			graceful = make(chan *amqp.Error)

			err := m.connect()
			for err != nil {
				err = m.connect()
			}
			errs = m.channel.NotifyClose(graceful)
		}
	}

	fmt.Println("reconnect finish")
}

func (m *MQ) getDSN() string {
	host := Config.GetString("mq", "host")
	protocol := Config.GetString("mq", "protocol")
	user := Config.GetString("mq", "user")
	password := Config.GetString("mq", "password")
	port := Config.GetString("mq", "port")
	dsn := strings.Join([]string{protocol, "://", user, ":", password, "@", host, ":", port, "/"}, "")
	fmt.Println("mq dsn -----------------------------------:", dsn)
	return dsn
}

func (m *MQ) connect() (err error) {
	m.con, err = amqp.Dial(m.dsn)
	if err != nil {
		//logging.Error("rabbitmq 连接失败")
		return err
	} else {
		logging.Debug("rabbitmq 连接成功")
	}
	err, ch := m.GetChannel()
	if err != nil {
		panic(err)
	}
	m.channel = ch

	if len(m.customers) > 0 {
		for _, v := range m.customers {
			if v.IsStoped {
				v.StartWork()
			}
		}
	}
	return err
}

func (m *MQ) GetChannel() (err error, ch *amqp.Channel) {
	ch, err = m.con.Channel()
	if err != nil {
		err = errors.New("rabbitMQ channel err:" + err.Error())
		return err, ch
	}
	return err, ch
}

func (m *MQ) QueueDeclare(queueName string, args map[string]interface{}) (error, *amqp.Queue) {
	q, err := m.channel.QueueDeclare(queueName, true, false, false, false, args)
	if err != nil {
		err = errors.New("rabbitMQ QueueDeclare err:" + err.Error())
		return err, nil
	}
	return err, &q
}

func (m *MQ) Produce(queueName string, req interface{}, args map[string]interface{}) (err error) {
	//err, ch := m.GetChannel()
	//if err != nil {
	//	return err
	//}
	err, _ = m.QueueDeclare(queueName, args)
	if err != nil {
		return err
	}
	//m.Name = queueName
	//m.args = args
	body, _ := json.Marshal(req) // title，content
	err = m.channel.Publish("", queueName, false, false, amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		Body:         body,
	})
	if err != nil {
		err = errors.New("rabbitMQ publish err:" + err.Error())
		return err
	}
	return nil
}

func (m *MQ) Consume(queueName string, f func(<-chan amqp.Delivery, *RabbitMqCustomer), autoAck bool, args map[string]interface{}) (err error) {
	fmt.Println("queueName..", queueName)
	if _, Ok := m.customers[queueName]; Ok {
		if m.customers[queueName].IsStoped {
			m.customers[queueName].StartWork()
		}
	} else {
		customer := NewRabbitMqCustomer(queueName, m, queueName, f, autoAck, args)
		m.customers[queueName] = customer
		m.customers[queueName].StartWork()
	}
	return err
}

func GetMQ() *MQ {
	return mqInstance
}

var once sync.Once

func MQInit() {
	if Config != nil {
		once.Do(func() {
			mqInstance = new(MQ)
			mqInstance.init()
		})

	} else {
		panic(errors.New("mq配置未初始化"))
	}
}

type RabbitMqCustomer struct {
	id        string
	m         *MQ
	queueName string
	workFunc  func(<-chan amqp.Delivery, *RabbitMqCustomer)
	autoAck   bool
	IsStoped  bool
	args      map[string]interface{}
}

func NewRabbitMqCustomer(id string, m *MQ, queueName string, f func(<-chan amqp.Delivery, *RabbitMqCustomer), autoAck bool, args map[string]interface{}) *RabbitMqCustomer {
	return &RabbitMqCustomer{
		id:        id,
		m:         m,
		queueName: queueName,
		workFunc:  f,
		autoAck:   autoAck,
		args:      args,
	}
}

func (that *RabbitMqCustomer) StartWork() error {
	err, _ := that.m.QueueDeclare(that.queueName, that.args)
	if err != nil {
		return err
	}
	fmt.Println("start custom 2222222222.", err)
	msgs, err := that.m.channel.Consume(that.queueName, "", that.autoAck, false, false, false, nil)
	if err != nil {
		fmt.Println("Consume err: ", err)
		panic(err)
	}
	that.IsStoped = false
	// 处于一个监听状态，一致监听我们的生产端的生产，所以这里我们要阻塞主进程
	fmt.Println("消费者启动", that.id)
	that.workFunc(msgs, that)
	return err
}

type RabbitMqProducer struct {
	QueueName string
	Args      map[string]interface{}
}

func NewRabbitMqProducer(queueName string) *RabbitMqProducer {
	args := map[string]interface{}{"x-message-ttl": 10000}
	return &RabbitMqProducer{
		QueueName: queueName,
		Args:      args,
	}
}
func (ai *RabbitMqProducer) Produce(mqReq interface{}) error {
	mq := GetMQ()

	err := mq.Produce(ai.QueueName, mqReq, ai.Args)
	return err
}
