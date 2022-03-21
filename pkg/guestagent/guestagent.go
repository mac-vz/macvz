package guestagent

type Agent interface {
	PublishInfo()
	ListenAndSendEvents()
}
