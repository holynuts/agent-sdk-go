package config

// SurrealDBConfig holds the configuration for connecting to SurrealDB.
type SurrealDBConfig struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	NS       string `yaml:"ns"`
	DB       string `yaml:"db"`
}
