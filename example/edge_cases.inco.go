package example

import "fmt"

// --- Case 1: Closure with @inco: ---

func ProcessWithCallback(db *DB) {
	handler := func(u *User) {
		// @inco: u != nil
		fmt.Println(u.Name)
	}

	u, _ := db.Query("SELECT 1")
	handler(u)
}

// --- Case 2: Custom panic with fmt.Sprintf ---

func FindUser(db *DB, id string) (*User, error) {
	// @inco: db != nil, -panic("db is nil")
	// @inco: len(id) > 0, -panic(fmt.Sprintf("invalid id: %q", id))
	user, err := db.Query("SELECT * FROM users WHERE id = ?")
	if err != nil {
		return nil, err
	}
	return user, nil
}

// --- Case 3: Multiple directives on same function ---

func MultiCheck(a, b int, name string) {
	// @inco: a > 0, -panic("a must be positive")
	// @inco: b < 1000, -panic("b overflow")
	// @inco: len(name) > 0

	fmt.Println(a, b, name)
}

// --- Case 4: -return action ---

func SafeDivide(a, b int) (int, error) {
	// @inco: b != 0, -return(0, fmt.Errorf("division by zero"))
	return a / b, nil
}

// --- Case 5: -continue in loop ---

func PrintPositive(nums []int) {
	for _, n := range nums {
		// @inco: n > 0, -continue
		fmt.Println(n)
	}
}

// --- Case 6: -break in loop ---

func FindFirst(nums []int, target int) int {
	for i, n := range nums {
		// @inco: n != target, -break
		_ = i
	}
	return -1
}

// --- Case 7: Nested closure ---

func NestedClosure() {
	outer := func() {
		inner := func(x int) {
			// @inco: x > 0
			fmt.Println(x)
		}
		inner(1)
	}
	outer()
}

// --- Case 8: Bare return ---

func Guard(x int) {
	// @inco: x >= 0, -return
	fmt.Println(x)
}

// --- Case 9: @inco: in same function with regular if ---

func SafeSlice(s []int, start, end int) []int {
	// @inco: start >= 0
	// @inco: end <= len(s)
	if start > end {
		return nil
	}
	return s[start:end]
}
