package middleware

import (
	"sync"
	"time"
)

// LoginThrottle tracks failed login attempts per email and temporarily locks accounts.
type LoginThrottle struct {
	mu          sync.Mutex
	attempts    map[string]*loginAttempt
	maxFailures int
	lockoutTime time.Duration
}

type loginAttempt struct {
	failures int
	lockedAt time.Time
}

func NewLoginThrottle(maxFailures int, lockoutTime time.Duration) *LoginThrottle {
	lt := &LoginThrottle{
		attempts:    make(map[string]*loginAttempt),
		maxFailures: maxFailures,
		lockoutTime: lockoutTime,
	}
	go lt.cleanup()
	return lt
}

// IsLocked returns true if the email is currently locked out.
func (lt *LoginThrottle) IsLocked(email string) bool {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	a, exists := lt.attempts[email]
	if !exists {
		return false
	}
	if a.failures >= lt.maxFailures && time.Since(a.lockedAt) < lt.lockoutTime {
		return true
	}
	if a.failures >= lt.maxFailures && time.Since(a.lockedAt) >= lt.lockoutTime {
		delete(lt.attempts, email)
		return false
	}
	return false
}

// RecordFailure records a failed login attempt. Returns true if the account is now locked.
func (lt *LoginThrottle) RecordFailure(email string) bool {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	a, exists := lt.attempts[email]
	if !exists {
		a = &loginAttempt{}
		lt.attempts[email] = a
	}
	a.failures++
	if a.failures >= lt.maxFailures {
		a.lockedAt = time.Now()
		return true
	}
	return false
}

// RecordSuccess clears failed attempts on successful login.
func (lt *LoginThrottle) RecordSuccess(email string) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	delete(lt.attempts, email)
}

func (lt *LoginThrottle) cleanup() {
	for {
		time.Sleep(5 * time.Minute)
		lt.mu.Lock()
		for email, a := range lt.attempts {
			if time.Since(a.lockedAt) > lt.lockoutTime*2 {
				delete(lt.attempts, email)
			}
		}
		lt.mu.Unlock()
	}
}
