package config

type MongoOption func(o *MongoOptions)
type MongoOptions struct {
	URI            string
	Database       string
	Username       string
	Password       string
	AuthSource     string
	ConnectTimeout int
	MaxPoolSize    uint64
}

func NewMongoOptions(opts ...MongoOption) MongoOptions {
	opt := MongoOptions{}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}

func MongoURI(v string) MongoOption {
	return func(o *MongoOptions) {
		o.URI = v
	}
}

func MongoDatabase(v string) MongoOption {
	return func(o *MongoOptions) {
		o.Database = v
	}
}

func MongoUsername(v string) MongoOption {
	return func(o *MongoOptions) {
		o.Username = v
	}
}

func MongoPassword(v string) MongoOption {
	return func(o *MongoOptions) {
		o.Password = v
	}
}

func MongoAuthSource(v string) MongoOption {
	return func(o *MongoOptions) {
		o.AuthSource = v
	}
}

func MongoConnectTimeout(v int) MongoOption {
	return func(o *MongoOptions) {
		o.ConnectTimeout = v
	}
}

func MongoMaxPoolSize(v uint64) MongoOption {
	return func(o *MongoOptions) {
		o.MaxPoolSize = v
	}
}
