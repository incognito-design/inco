package example

import "fmt"

type Account struct {
	ID      string
	Balance int
}

type QueryResult struct {
	RowsAffected int
}

type dbConn struct{}

func (d *dbConn) Exec(query string) (*QueryResult, error) {
	return &QueryResult{RowsAffected: 1}, nil
}

var db = &dbConn{}

// Transfer demonstrates multiple @inco: with panic.
func Transfer(from *Account, to *Account, amount int) {
	// @inco: from != nil
	// @inco: to != nil
	// @inco: amount > 0, -panic("amount must be positive")

	query := fmt.Sprintf("UPDATE accounts SET balance = balance - %d WHERE id = '%s'", amount, from.ID)
	res, err := db.Exec(query)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Transfer %d from %s to %s, affected %d rows\n",
		amount, from.ID, to.ID, res.RowsAffected)
}
