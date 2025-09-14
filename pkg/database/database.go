package database

import (
	"log"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Init opens a PostgreSQL connection using DATABASE_URL
// and returns a *gorm.DB instance.
// If the connection fails, the app will exit with log.Fatal.
func Init() *gorm.DB {
	dsn := os.Getenv("DATABASE_URL")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// Example: set naming strategy if needed
		// NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		log.Fatal("failed to connect database:", err)
	}
	return db
}
