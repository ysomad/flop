package flop_test

import (
	"fmt"

	"github.com/ysomad/flop"
)

func ExampleSchema_CompileFilter() {
	schema := flop.NewSchema(flop.NewField("name").String().Filterable()).MustBuild()
	filter, err := schema.ParseFilter(`name = 'Alice'`)
	if err != nil {
		panic(err)
	}

	expr, err := schema.CompileFilter(filter)
	if err != nil {
		panic(err)
	}

	sql, args, err := flop.WhereExprSQL(expr)
	if err != nil {
		panic(err)
	}

	fmt.Println(sql, args["name_1"])
	// Output: (name = @name_1) Alice
}

func ExampleNewOffsetPage() {
	page, err := flop.NewOffsetPage([]string{"third", "fourth"}, 2, 2, 5)
	if err != nil {
		panic(err)
	}
	fmt.Println(page.Items, page.Page, page.TotalPages, page.TotalItems)
	// Output: [third fourth] 2 3 5
}
