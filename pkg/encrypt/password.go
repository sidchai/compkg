package encrypt

import (
	"golang.org/x/crypto/bcrypt"
	"log"
)

func Encrypt(password string) string {

	bs, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		// never runs here
		log.Printf("[NRH] encrypt failed error: %v", err)
		return ""
	}

	return string(bs)
}

func Compare(hashedPassword, password string) bool {

	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))

	if err == bcrypt.ErrMismatchedHashAndPassword {
		return false
	}

	if err != nil {
		// never runs here
		log.Printf("[NRH] compare failed error: %v", err)
		return false
	}

	return true
}
