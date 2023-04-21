package notify

type Notifier interface {
	Collect()
	Send() error
}
