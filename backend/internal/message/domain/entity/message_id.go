package entity

import "regexp"

// messageIDPattern is the ID shape the ID generator produces: the prefix and 26 Crockford base32
// characters (api/openapi.yaml, schema MessageID).
var messageIDPattern = regexp.MustCompile(`^msg_[0-9A-HJKMNP-TV-Z]{26}$`)

// IsMessageID reports whether s is a well-formed message ID. Delivery answers 404 for anything
// else, so a malformed ID never reaches the database.
func IsMessageID(s string) bool { return messageIDPattern.MatchString(s) }
