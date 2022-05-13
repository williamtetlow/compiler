package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/profile"
	astro "github.com/withastro/compiler/internal"
	"github.com/withastro/compiler/internal/printer"
	"github.com/withastro/compiler/internal/transform"
)

func main() {
	defer profile.Start(profile.MemProfile).Stop()
	// time.Sleep(time.Second * 10)
	source := `---
	import {format} from 'date-fns'; 
	
	// Welcome to Astro!
	// Write JavaScript & TypeScript here, in the "component script."
	// This will run during the build, but never in the final output.
	// Use these variables in the HTML template below.
	//
	// Full Syntax:
	// https://docs.astro.build/core-concepts/astro-components/
	
	const builtAt: Date = new Date();
	const builtAtFormatted = format(builtAt, 'MMMM dd, yyyy -- H:mm:ss.SSS');
	---
	<html lang="en">
		<head>
			<meta charset="UTF-8">
			<title>Astro Playground</title>
			<style>
				header {
					display: flex;
					flex-direction: column;
					align-items: center;
					text-align: center;
					margin-top: 15vh;
					font-family: Arial;
				}
				.note {
					margin: 0;
					padding: 1rem;
					border-radius: 8px;
					background: #E4E5E6;
					border: 1px solid #BBB;
				}
			</style>
		</head>
		<body>
			<header>
				<img width="60" height="80" src="https://bestofjs.org/logos/astro.svg" alt="Astro logo">
				<h1>Hello, Astro!</h1>
				<p class="note">
					<strong>RENDERED AT:</strong><br/>
					{builtAtFormatted}
				</p>
			</header>
		</body>
	</html>	
	`
	// s := source
	// _ = s
	doc, err := astro.Parse(strings.NewReader(source))
	if err != nil {
		fmt.Println(err)
		return
	}
	hash := astro.HashFromSource(source)

	transform.ExtractStyles(doc)
	transform.Transform(doc, transform.TransformOptions{
		Scope: hash,
	})

	result := printer.PrintToJS(source, doc, 0, transform.TransformOptions{})

	content, _ := json.Marshal(source)
	sourcemap := `{ "version": 3, "sources": ["file.astro"], "names": [], "mappings": "` + string(result.SourceMapChunk.Buffer) + `", "sourcesContent": [` + string(content) + `] }`
	b64 := base64.StdEncoding.EncodeToString([]byte(sourcemap))
	output := string(result.Output) + string('\n') + `//# sourceMappingURL=data:application/json;base64,` + b64 + string('\n')
	fmt.Print(output)
}

// 	// z := astro.NewTokenizer(strings.NewReader(source))

// 	// for {
// 	// 	if z.Next() == astro.ErrorToken {
// 	// 		// Returning io.EOF indicates success.
// 	// 		return
// 	// 	}
// 	// tok := z.Token()

// 	// if tok.Type == astro.StartTagToken {
// 	// 	for _, attr := range tok.Attr {
// 	// 		switch attr.Type {
// 	// 		case astro.ShorthandAttribute:
// 	// 			fmt.Println("ShorthandAttribute", attr.Key, attr.Val)
// 	// 		case astro.ExpressionAttribute:
// 	// 			if strings.Contains(attr.Val, "<") {
// 	// 				fmt.Println("ExpressionAttribute with Elements", attr.Val)
// 	// 			} else {
// 	// 				fmt.Println("ExpressionAttribute", attr.Key, attr.Val)
// 	// 			}
// 	// 		case astro.QuotedAttribute:
// 	// 			fmt.Println("QuotedAttribute", attr.Key, attr.Val)
// 	// 		case astro.SpreadAttribute:
// 	// 			fmt.Println("SpreadAttribute", attr.Key, attr.Val)
// 	// 		case astro.TemplateLiteralAttribute:
// 	// 			fmt.Println("TemplateLiteralAttribute", attr.Key, attr.Val)
// 	// 		}
// 	// 	}
// 	// }
// 	// }
// }

// func Transform(source string) interface{} {
// 	doc, _ := astro.ParseFragment(strings.NewReader(source), nil)

// 	for _, node := range doc {
// 		fmt.Println(node.Data)
// 	}
// 	// hash := hashFromSource(source)

// 	// transform.Transform(doc, transform.TransformOptions{
// 	// 	Scope: hash,
// 	// })

// 	// w := new(strings.Builder)
// 	// astro.Render(w, doc)
// 	// js := w.String()

// 	// return js
// 	return nil
// }
