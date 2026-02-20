package example

import "fmt"

type User struct {
	Name string
	Age  int
}

type DB struct {
	connected bool
}

func (db *DB) Query(q string) (*User, error) {
	return &User{Name: "test", Age: 25}, nil
}

// --- Case 1: Default action (panic) with expression ---

func CreateUser(name string, age int) {
	// @inco: len(name) > 0
	// @inco: age > 0

	fmt.Printf("Creating user: %s, age %d\n", name, age)
}

// --- Case 2: Panic with custom message ---

func GetUser(u *User) {
	// @inco: u != nil, -panic("user must not be nil")

	fmt.Println(u.Name)
}

// --- Case 3: Multiple preconditions ---

func FetchUser(db *DB, id string) (*User, error) {
	// @inco: db != nil
	// @inco: len(id) > 0, -panic("empty id")

	user, err := db.Query("SELECT * FROM users WHERE id = ?")
	if err != nil {
		return nil, err
	}
	return user, nil
}
