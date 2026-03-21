package events

// Routing keys used when publishing events to RabbitMQ.
const (
	TopicUserCreated = "user.created"
	TopicUserDeleted = "user.deleted"
	TopicAuthLogin   = "auth.login"
	TopicAuthLogout  = "auth.logout"
)
