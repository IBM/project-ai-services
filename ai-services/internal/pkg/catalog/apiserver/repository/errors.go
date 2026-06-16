package repository

// Error message constants for application operations.
const (
	// ErrMsgApplicationNotFound is returned when an application does not exist.
	ErrMsgApplicationNotFound = "application does not exist"

	// ErrMsgUserNotOwner is returned when a user does not own the application.
	ErrMsgUserNotOwner = "user does not own this application"

	// ErrMsgApplicationAlreadyDeleting is returned when an application is already being deleted.
	ErrMsgApplicationAlreadyDeleting = "application is already being deleted"

	// ErrMsgApplicationNameExists is returned when an application with the given name already exists.
	ErrMsgApplicationNameExists = "application with name '%s' already exists"
)

// Made with Bob
