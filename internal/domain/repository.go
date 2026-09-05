package domain

type ServiceRepository interface {
	Register(Service) Service
	Deregister(name string, now int64) bool
	Find(name string) []Service
	List() []Service
}
