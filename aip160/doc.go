// Package aip160 parses AIP-160 filter expressions into an abstract syntax
// tree.
//
// The grammar is at https://google.aip.dev/assets/misc/ebnf-filtering.txt.
// Function call syntax is not supported.
//
// The parser is copied from go.chromium.org/luci/common/data/aip160; see
// filter_parser.go for the modifications made for flop.
package aip160
