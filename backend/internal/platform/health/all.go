package health

import "context"

// allPingers is a Pinger that needs every member to answer.
type allPingers []Pinger

// All returns a Pinger that pings each member in order and returns the first error. The worker
// uses it to require both the database and a recent claim.
func All(pingers ...Pinger) Pinger { return allPingers(pingers) }

// Ping implements Pinger.
func (a allPingers) Ping(ctx context.Context) error {
	for _, p := range a {
		if err := p.Ping(ctx); err != nil {
			return err
		}
	}
	return nil
}
