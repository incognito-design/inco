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

// GetUser demonstrates @require -nd (non-defaulted precondition)
func GetUser(u *User, db *DB) {
	// @require -nd u, db

	fmt.Println(u.Name)
}

// CreateUser demonstrates @require with expression
func CreateUser(name string, age int) {
	// @require len(name) > 0, "name must not be empty"
	// @require age > 0

	fmt.Printf("Creating user: %s, age %d\n", name, age)
}

// FetchUser demonstrates @must (error must be nil)
func FetchUser(db *DB) *User {
	res, _ := db.Query("SELECT * FROM users") // @must

	fmt.Println("Fetched:", res.Name)
	return res
}

// SafeProcess demonstrates @ensure -nd (postcondition)
func SafeProcess(id string) (result *User) {
	// @ensure -nd result

	if id == "valid" {
		return &User{Name: "found"}
	}
	return nil // this will trigger the ensure violation!
}
