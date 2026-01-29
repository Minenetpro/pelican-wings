package config

// AxiomConfiguration defines the configuration for the Axiom event ingest
// integration. When enabled, server events (stats, status changes, and console
// output) are batched and POSTed to the Axiom ingest API.
type AxiomConfiguration struct {
	// Whether the Axiom integration is enabled.
	Enabled bool `default:"false" yaml:"enabled"`

	// The base URL of the Axiom API (e.g. "https://api.axiom.co").
	URL string `yaml:"url"`

	// The API token used to authenticate with Axiom.
	APIToken string `yaml:"api_token"`

	// The name of the Axiom dataset to ingest events into.
	Dataset string `yaml:"dataset"`

	// How often (in seconds) the event buffer is flushed to Axiom.
	FlushInterval int `default:"5" yaml:"flush_interval"`

	// The maximum number of events to accumulate before triggering a flush.
	BatchSize int `default:"100" yaml:"batch_size"`
}
