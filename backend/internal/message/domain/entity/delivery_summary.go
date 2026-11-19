package entity

// DeliverySummary is how many deliveries of a message are pending, succeeded and failed.
type DeliverySummary struct {
	Pending   int
	Succeeded int
	Failed    int
}
