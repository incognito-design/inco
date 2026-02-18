package example

import "fmt"

// --- Case 1: Anonymous function (closure) with @require ---

func ProcessWithCallback(db *DB) {
	handler := func(u *User) {
		// @require -nd u
		fmt.Println(u.Name)
	}

	u, _ := db.Query("SELECT 1")
	handler(u)
}

// --- Case 2: Multi-line @must (directive on its own line, statement spans multiple lines) ---

func FetchMultiLine(db *DB) *User {
	// @must
	res, _ := db.Query(
		"SELECT * FROM users WHERE id = ?",
	)

	fmt.Println("Fetched:", res.Name)
	return res
}

// --- Case 3: Nested closure with @ensure ---

func WithEnsure() (result *User) {
	// @ensure -nd result

	compute := func() *User {
		// @require -nd result
		return &User{Name: "inner"}
	}

	_ = compute
	return nil
}
