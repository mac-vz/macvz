package guestagent

type Agent interface {
	PublishInfo()
	StartDNS()
	ListenAndSendEvents()
}
