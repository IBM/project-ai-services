package models

type User struct {
	ID           string
	UserName     string
	PasswordHash string
	Name         string
	// IDP is the identity provider that authenticated the user ("local" or "miq").
	// It is set by the server, never taken from a client, and is part of the
	// identity the service-token resolver keys on.
	IDP string
}
