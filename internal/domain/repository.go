package domain

type ServiceRepository interface {
	Register(Service) Service
	Deregister(name, id string, now int64) bool
	Heartbeat(name, id string, health HealthStatus, at int64) (Service, bool)
	Find(name, id string) []Service
	List() []Service
}
