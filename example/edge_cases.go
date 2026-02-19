package example

import "fmt"

// --- Case 1: Closure with @require ---

func ProcessWithCallback(db *DB) {
	handler := func(u *User) {
		// @require u != nil
		fmt.Println(u.Name)
	}

	u, _ := db.Query("SELECT 1")
	handler(u)
}

// --- Case 2: @must with custom panic message ---

func FindUser(db *DB, id string) (*User, error) {
	// @require db != nil panic("db is nil")
	// @require len(id) > 0 panic(fmt.Sprintf("invalid id: %q", id))
	user, _ := db.Query("SELECT * FROM users WHERE id = ?") // @must panic("query failed")
	return user, nil
}

// --- Case 3: Multiple directives on same function ---

func MultiCheck(a, b int, name string) {
	// @require a > 0 panic("a must be positive")
	// @require b < 1000 panic("b overflow")
	// @require len(name) > 0

	fmt.Println(a, b, name)
}

// --- Case 4: @ensure for map lookup ---

func LookupKey(m map[string]int, key string) int {
	v, _ := m[key] // @ensure panic("key not found: " + key)
	return v
}

// --- Case 5: @ensure for type assertion ---

func MustString(x any) string {
	v, _ := x.(string) // @ensure panic("not a string")
	return v
}

// --- Case 6: Same-function multiple @must (`:=` then `=`) ---

func MultiMust(db *DB) {
	u1, _ := db.Query("q1") // @must
	u2, _ := db.Query("q2") // @must
	fmt.Println(u1, u2)
}

// --- Case 7: Mixed @must + @ensure in same function ---

func MixedDirectives(db *DB, m map[string]int) {
	user, _ := db.Query("q") // @must
	count, _ := m[user.Name] // @ensure panic("user not in map")
	fmt.Println(count)
}

// --- Case 8: Nested closure with @must ---

func NestedClosure(db *DB) {
	outer := func() {
		inner := func() {
			_, _ = db.Query("nested") // @must
		}
		inner()
	}
	outer()
}

// --- Case 9: Single-value @must (`_ = foo()` pattern) ---

func SingleValueMust() {
	_ = fmt.Errorf("test %d", 42) // @must
}

// --- Case 10: Variable name with underscore — must not be replaced ---

func UnderscoreInName(db *DB) {
	my_user, _ := db.Query("q") // @must
	fmt.Println(my_user)
}
