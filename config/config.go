package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds the application configuration
type Config struct {
	ESLAddr     string
	ESLPass     string
	DatabaseURL string
	APIPort     string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	// Attempt to load .env file, but don't fail if it's not present
	// This is useful for development environments
	_ = godotenv.Load()

	eslAddr := getEnv("ESL_ADDR", "127.0.0.1:8021")
	eslPass := getEnv("ESL_PASS", "ClueCon")
	dbURL := getEnv("DATABASE_URL", "postgresql://postgres:SRSwoqA2m6PDqmuC@db.nztuusrizgmjttoymidp.supabase.co:5432/postgres")
	apiPort := getEnv("API_PORT", "8080")

	return &Config{
		ESLAddr:     eslAddr,
		ESLPass:     eslPass,
		DatabaseURL: dbURL,
		APIPort:     apiPort,
	}
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	log.Printf("Using default value for %s: %s", key, defaultValue)
	return defaultValue
}

// GetAPIPortInt returns the API port as an integer
func (c *Config) GetAPIPortInt() int {
	port, err := strconv.Atoi(c.APIPort)
	if err != nil {
		log.Fatalf("Invalid API_PORT: %v", err)
	}
	return port
}
