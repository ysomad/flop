package flopsq_test

import (
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/ysomad/flop"
	"github.com/ysomad/flop/flopsq"
)

func ExampleCursorQuery() {
	if err := quickstart(); err != nil {
		panic(err)
	}
	// Output:
	// SELECT id, amount FROM payments WHERE amount >= $1 ORDER BY amount DESC, id LIMIT 3 [100]
	// [{1 300} {2 200}] true
}

func quickstart() error {
	type payment struct{ ID, Amount int64 }
	schema, err := flop.NewSchema(
		flop.NewField("id").Int().Unique().Value(func(p payment) any { return p.ID }),
		flop.NewField("amount").Int().Filterable().Sortable().Value(func(p payment) any { return p.Amount }),
	).Build()
	if err != nil {
		return err
	}
	filter, err := schema.ParseFilter("amount >= 100")
	if err != nil {
		return err
	}
	defaults, err := schema.ParseOrder("amount desc")
	if err != nil {
		return err
	}
	requested, err := schema.ParseOrder("")
	if err != nil {
		return err
	}
	order := schema.TotalOrder(flop.MergeOrder(defaults, requested))
	after, err := schema.DecodeCursor("", order, filter) // First page.
	if err != nil {
		return err
	}
	base := sq.Select("id", "amount").From("payments").PlaceholderFormat(sq.Dollar)
	query, err := flopsq.CursorQuery(base, schema, order, filter, after, 2, 0)
	if err != nil {
		return err
	}
	sql, args, err := query.ToSql()
	if err != nil {
		return err
	}
	fmt.Println(sql, args)
	// Replace these sample rows with the query result, including its surplus row.
	rows := []payment{{ID: 1, Amount: 300}, {ID: 2, Amount: 200}, {ID: 3, Amount: 100}}
	page, err := schema.NewCursorPage(rows, 2, order, filter)
	if err != nil {
		return err
	}
	fmt.Println(page.Items, page.NextCursor != "")
	return nil
}
