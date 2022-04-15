package printer

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"

	astro "github.com/withastro/compiler/internal"
	types "github.com/withastro/compiler/internal/t"
	"github.com/withastro/compiler/internal/test_utils"
	"github.com/withastro/compiler/internal/transform"
)

var INTERNAL_IMPORTS = fmt.Sprintf("import {\n  %s\n} from \"%s\";\n", strings.Join([]string{
	FRAGMENT,
	"render as " + TEMPLATE_TAG,
	"createAstro as " + CREATE_ASTRO,
	"createComponent as " + CREATE_COMPONENT,
	"renderComponent as " + RENDER_COMPONENT,
	"unescapeHTML as " + UNESCAPE_HTML,
	"renderSlot as " + RENDER_SLOT,
	"addAttribute as " + ADD_ATTRIBUTE,
	"spreadAttributes as " + SPREAD_ATTRIBUTES,
	"defineStyleVars as " + DEFINE_STYLE_VARS,
	"defineScriptVars as " + DEFINE_SCRIPT_VARS,
	"createMetadata as " + CREATE_METADATA,
}, ",\n  "), "http://localhost:3000/")
var PRELUDE = fmt.Sprintf(`//@ts-ignore
const $$Component = %s(async ($$result, $$props, %s) => {
const Astro = $$result.createAstro($$Astro, $$props, %s);
Astro.self = $$Component;%s`, CREATE_COMPONENT, SLOTS, SLOTS, "\n")
var RETURN = fmt.Sprintf("return %s%s", TEMPLATE_TAG, BACKTICK)
var SUFFIX = fmt.Sprintf("%s;", BACKTICK) + `
});
export default $$Component;`
var STYLE_PRELUDE = "const STYLES = [\n"
var STYLE_SUFFIX = "];\nfor (const STYLE of STYLES) $$result.styles.add(STYLE);\n"
var SCRIPT_PRELUDE = "const SCRIPTS = [\n"
var SCRIPT_SUFFIX = "];\nfor (const SCRIPT of SCRIPTS) $$result.scripts.add(SCRIPT);\n"
var CREATE_ASTRO_CALL = "const $$Astro = $$createAstro(import.meta.url, 'https://astro.build', '.');\nconst Astro = $$Astro;"
var RENDER_HEAD_RESULT = "<!--astro:head-->"

// SPECIAL TEST FIXTURES
var NON_WHITESPACE_CHARS = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[];:'\",.?")

type want struct {
	frontmatter    []string
	styles         []string
	scripts        []string
	getStaticPaths string
	code           string
	skipHoist      bool // HACK: sometimes `getStaticPaths()` appears in a slightly-different location. Only use this if needed!
	metadata
}

type metadata struct {
	hoisted              []string
	hydratedComponents   []string
	clientOnlyComponents []string
	modules              []string
	hydrationDirectives  []string
}

type testcase struct {
	name             string
	source           string
	only             bool
	staticExtraction bool
	want             want
}

type jsonTestcase struct {
	name   string
	source string
	want   []ASTNode
}

func TestPrinter(t *testing.T) {
	longRandomString := ""
	for i := 0; i < 4080; i++ {
		longRandomString += string(NON_WHITESPACE_CHARS[rand.Intn(len(NON_WHITESPACE_CHARS))])
	}

	tests := []testcase{
		{
			name:   "basic (no frontmatter)",
			source: `<button>Click</button>`,
			want: want{
				code: `<button>Click</button>`,
			},
		},
		{
			name:   "basic renderHead",
			source: `<html><head><title>Ah</title></head></html>`,
			want: want{
				code: `<html><head><title>Ah</title>` + RENDER_HEAD_RESULT + `</head></html>`,
			},
		},
		{
			name:   "head slot",
			source: `<html><head><slot /></html>`,
			want: want{
				code: `<html><head>${$$renderSlot($$result,$$slots["default"])}` + RENDER_HEAD_RESULT + `</head></html>`,
			},
		},
		{
			name:   "head slot II",
			source: `<html><head><slot /></head><body class="a"></body></html>`,
			want: want{
				code: `<html><head>${$$renderSlot($$result,$$slots["default"])}` + RENDER_HEAD_RESULT + `</head><body class="a"></body></html>`,
			},
		},
		{
			name: "basic (frontmatter)",
			source: `---
const href = '/about';
---
<a href={href}>About</a>`,
			want: want{
				frontmatter: []string{"", "const href = '/about';"},
				code:        `<a${` + ADD_ATTRIBUTE + `(href, "href")}>About</a>`,
			},
		},
		{
			name: "getStaticPaths (basic)",
			source: `---
export const getStaticPaths = async () => {
	return { paths: [] }
}
---
<div></div>`,
			want: want{
				frontmatter: []string{`export const getStaticPaths = async () => {
	return { paths: [] }
}`, ""},
				code: `<div></div>`,
			},
		},
		{
			name: "getStaticPaths (hoisted)",
			source: `---
const a = 0;
export const getStaticPaths = async () => {
	return { paths: [] }
}
---
<div></div>`,
			want: want{
				frontmatter: []string{"", `const a = 0;`},
				getStaticPaths: `export const getStaticPaths = async () => {
	return { paths: [] }
}`,
				code: `<div></div>`,
			},
		},
		{
			name: "getStaticPaths (hoisted II)",
			source: `---
const a = 0;
export async function getStaticPaths() {
	return { paths: [] }
}
const b = 0;
---
<div></div>`,
			want: want{
				frontmatter: []string{"", `const a = 0;
const b = 0;`},
				getStaticPaths: `export async function getStaticPaths() {
	return { paths: [] }
}`,
				code: `<div></div>`,
			},
		},
		{
			name: "export member does not panic",
			source: `---
mod.export();
---
<div />`,
			want: want{
				frontmatter: []string{``, `mod.export();`},
				code:        `<div></div>`,
			},
		},
		{
			name: "import assertions",
			source: `---
import data from "test" assert { type: 'json' };
---
`,
			want: want{
				frontmatter: []string{
					`import data from "test" assert { type: 'json' };`,
				},
				metadata: metadata{modules: []string{`{ module: $$module1, specifier: 'test', assert: {type:'json'} }`}},
				styles:   []string{},
			},
		},
		{
			name:   "solidus in template literal expression",
			source: "<div value={`${attr ? `a/b` : \"c\"} awesome`} />",
			want: want{
				code: "<div${$$addAttribute(`${attr ? `a/b` : \"c\"} awesome`, \"value\")}></div>",
			},
		},
		{
			name:   "nested template literal expression",
			source: "<div value={`${attr ? `a/b ${`c`}` : \"d\"} awesome`} />",
			want: want{
				code: "<div${$$addAttribute(`${attr ? `a/b ${`c`}` : \"d\"} awesome`, \"value\")}></div>",
			},
		},
		{
			name:   "complex nested template literal expression",
			source: "<div value={`${attr ? `a/b ${`c ${`d ${cool}`}`}` : \"d\"} ahhhh`} />",
			want: want{
				code: "<div${$$addAttribute(`${attr ? `a/b ${`c ${`d ${cool}`}`}` : \"d\"} ahhhh`, \"value\")}></div>",
			},
		},
		{
			name: "component",
			source: `---
import VueComponent from '../components/Vue.vue';
---
<html>
  <head>
    <title>Hello world</title>
  </head>
  <body>
    <VueComponent />
  </body>
</html>`,
			want: want{
				frontmatter: []string{
					`import VueComponent from '../components/Vue.vue';`,
				},
				metadata: metadata{modules: []string{`{ module: $$module1, specifier: '../components/Vue.vue', assert: {} }`}},
				code: `<html>
  <head>
    <title>Hello world</title>
  ` + RENDER_HEAD_RESULT + `</head>
  <body>
    ${` + RENDER_COMPONENT + `($$result,'VueComponent',VueComponent,{})}
  </body></html>
  `,
			},
		},
		{
			name: "dot component",
			source: `---
import * as ns from '../components';
---
<html>
  <head>
    <title>Hello world</title>
  </head>
  <body>
    <ns.Component />
  </body>
</html>`,
			want: want{
				frontmatter: []string{`import * as ns from '../components';`},
				styles:      []string{},
				metadata:    metadata{modules: []string{`{ module: $$module1, specifier: '../components', assert: {} }`}},
				code: `<html>
  <head>
    <title>Hello world</title>
  ` + RENDER_HEAD_RESULT + `</head>
  <body>
    ${` + RENDER_COMPONENT + `($$result,'ns.Component',ns.Component,{})}
  </body></html>
  `,
			},
		},
		{
			name: "noscript component",
			source: `
<html>
  <head></head>
  <body>
	<noscript>
		<Component />
	</noscript>
  </body>
</html>`,
			want: want{
				code: `<html>
  <head>` + RENDER_HEAD_RESULT + `</head>
  <body>
	<noscript>
		${` + RENDER_COMPONENT + `($$result,'Component',Component,{})}
	</noscript>
  </body></html>
  `,
			},
		},
		{
			name: "client:only component (default)",
			source: `---
import Component from '../components';
---
<html>
  <head>
    <title>Hello world</title>
  </head>
  <body>
    <Component client:only />
  </body>
</html>`,
			want: want{
				frontmatter: []string{"import Component from '../components';"},
				metadata: metadata{
					hydrationDirectives:  []string{"only"},
					clientOnlyComponents: []string{"../components"},
				},
				// Specifically do NOT render any metadata here, we need to skip this import
				code: `<html>
  <head>
    <title>Hello world</title>
  ` + RENDER_HEAD_RESULT + `</head>
  <body>
    ${` + RENDER_COMPONENT + `($$result,'Component',null,{"client:only":true,"client:component-hydration":"only","client:component-path":($$metadata.resolvePath("../components")),"client:component-export":"default"})}
  </body></html>`,
			},
		},
		{
			name: "client:only component (named)",
			source: `---
import { Component } from '../components';
---
<html>
  <head>
    <title>Hello world</title>
  </head>
  <body>
    <Component client:only />
  </body>
</html>`,
			want: want{
				frontmatter: []string{"import { Component } from '../components';"},
				metadata: metadata{
					hydrationDirectives:  []string{"only"},
					clientOnlyComponents: []string{"../components"},
				},
				// Specifically do NOT render any metadata here, we need to skip this import
				code: `<html>
  <head>
    <title>Hello world</title>
  ` + RENDER_HEAD_RESULT + `</head>
  <body>
    ${` + RENDER_COMPONENT + `($$result,'Component',null,{"client:only":true,"client:component-hydration":"only","client:component-path":($$metadata.resolvePath("../components")),"client:component-export":"Component"})}
  </body></html>`,
			},
		},
		{
			name: "client:only component (namespace)",
			source: `---
import * as components from '../components';
---
<html>
  <head>
    <title>Hello world</title>
  </head>
  <body>
    <components.A client:only />
  </body>
</html>`,
			want: want{
				frontmatter: []string{"import * as components from '../components';"},
				metadata: metadata{
					hydrationDirectives:  []string{"only"},
					clientOnlyComponents: []string{"../components"},
				},
				// Specifically do NOT render any metadata here, we need to skip this import
				code: `<html>
  <head>
    <title>Hello world</title>
  ` + RENDER_HEAD_RESULT + `</head>
  <body>
    ${` + RENDER_COMPONENT + `($$result,'components.A',null,{"client:only":true,"client:component-hydration":"only","client:component-path":($$metadata.resolvePath("../components")),"client:component-export":"A"})}
  </body></html>`,
			},
		},
		{
			name: "client:only component (multiple)",
			source: `---
import Component from '../components';
---
<html>
  <head>
    <title>Hello world</title>
  </head>
  <body>
    <Component test="a" client:only />
	<Component test="b" client:only />
	<Component test="c" client:only />
  </body>
</html>`,
			want: want{
				frontmatter: []string{"import Component from '../components';"},
				metadata: metadata{
					hydrationDirectives:  []string{"only"},
					clientOnlyComponents: []string{"../components"},
				},
				// Specifically do NOT render any metadata here, we need to skip this import
				code: `<html>
  <head>
    <title>Hello world</title>
  ` + RENDER_HEAD_RESULT + `</head>
  <body>
    ${` + RENDER_COMPONENT + `($$result,'Component',null,{"test":"a","client:only":true,"client:component-hydration":"only","client:component-path":($$metadata.resolvePath("../components")),"client:component-export":"default"})}
	${` + RENDER_COMPONENT + `($$result,'Component',null,{"test":"b","client:only":true,"client:component-hydration":"only","client:component-path":($$metadata.resolvePath("../components")),"client:component-export":"default"})}
	${` + RENDER_COMPONENT + `($$result,'Component',null,{"test":"c","client:only":true,"client:component-hydration":"only","client:component-path":($$metadata.resolvePath("../components")),"client:component-export":"default"})}
  </body></html>`,
			},
		},
		{
			name:   "iframe",
			source: `<iframe src="something" />`,
			want: want{
				code: "<iframe src=\"something\"></iframe>",
			},
		},
		{
			name:   "conditional render",
			source: `<body>{false ? <div>#f</div> : <div>#t</div>}</body>`,
			want: want{
				code: "<body>${false ? $$render`<div>#f</div>` : $$render`<div>#t</div>`}</body>",
			},
		},
		{
			name:   "conditional noscript",
			source: `{mode === "production" && <noscript>Hello</noscript>}`,
			want: want{
				code: "${mode === \"production\" && $$render`<noscript>Hello</noscript>`}",
			},
		},
		{
			name:   "conditional iframe",
			source: `{bool && <iframe src="something">content</iframe>}`,
			want: want{
				code: "${bool && $$render`<iframe src=\"something\">content</iframe>`}",
			},
		},
		{
			name:   "simple ternary",
			source: `<body>{link ? <a href="/">{link}</a> : <div>no link</div>}</body>`,
			want: want{
				code: fmt.Sprintf(`<body>${link ? $$render%s<a href="/">${link}</a>%s : $$render%s<div>no link</div>%s}</body>`, BACKTICK, BACKTICK, BACKTICK, BACKTICK),
			},
		},
		{
			name: "map basic",
			source: `---
const items = [0, 1, 2];
---
<ul>
	{items.map(item => {
		return <li>{item}</li>;
	})}
</ul>`,
			want: want{
				frontmatter: []string{"", "const items = [0, 1, 2];"},
				code: fmt.Sprintf(`<ul>
	${items.map(item => {
		return $$render%s<li>${item}</li>%s;
	})}
</ul>`, BACKTICK, BACKTICK),
			},
		},
		{
			name:   "map without component",
			source: `<header><nav>{menu.map((item) => <a href={item.href}>{item.title}</a>)}</nav></header>`,
			want: want{
				code: fmt.Sprintf(`<header><nav>${menu.map((item) => $$render%s<a${$$addAttribute(item.href, "href")}>${item.title}</a>%s)}</nav></header>`, BACKTICK, BACKTICK),
			},
		},
		{
			name:   "map with component",
			source: `<header><nav>{menu.map((item) => <a href={item.href}>{item.title}</a>)}</nav><Hello/></header>`,
			want: want{
				code: fmt.Sprintf(`<header><nav>${menu.map((item) => $$render%s<a${$$addAttribute(item.href, "href")}>${item.title}</a>%s)}</nav>${$$renderComponent($$result,'Hello',Hello,{})}</header>`, BACKTICK, BACKTICK),
			},
		},
		{
			name: "map nested",
			source: `---
const groups = [[0, 1, 2], [3, 4, 5]];
---
<div>
	{groups.map(items => {
		return <ul>{
			items.map(item => {
				return <li>{item}</li>;
			})
		}</ul>
	})}
</div>`,
			want: want{
				frontmatter: []string{"", "const groups = [[0, 1, 2], [3, 4, 5]];"},
				styles:      []string{},
				code: fmt.Sprintf(`<div>
	${groups.map(items => {
		return %s<ul>${
			items.map(item => {
				return %s<li>${item}</li>%s;
			})
		}</ul>%s})}
</div>`, "$$render"+BACKTICK, "$$render"+BACKTICK, BACKTICK, BACKTICK),
			},
		},
		{
			name:   "backtick in HTML comment",
			source: "<body><!-- `npm install astro` --></body>",
			want: want{
				code: "<body><!-- \\`npm install astro\\` --></body>",
			},
		},
		{
			name:   "nested expressions",
			source: `<article>{(previous || next) && <aside>{previous && <div>Previous Article: <a rel="prev" href={new URL(previous.link, Astro.site).pathname}>{previous.text}</a></div>}{next && <div>Next Article: <a rel="next" href={new URL(next.link, Astro.site).pathname}>{next.text}</a></div>}</aside>}</article>`,
			want: want{
				code: `<article>${(previous || next) && $$render` + BACKTICK + `<aside>${previous && $$render` + BACKTICK + `<div>Previous Article: <a rel="prev"${$$addAttribute(new URL(previous.link, Astro.site).pathname, "href")}>${previous.text}</a></div>` + BACKTICK + `}${next && $$render` + BACKTICK + `<div>Next Article: <a rel="next"${$$addAttribute(new URL(next.link, Astro.site).pathname, "href")}>${next.text}</a></div>` + BACKTICK + `}</aside>` + BACKTICK + `}</article>`,
			},
		},
		{
			name: "expressions with JS comments",
			source: `---
const items = ['red', 'yellow', 'blue'];
---
<div>
  {items.map((item) => (
    // foo < > < }
    <div id={color}>color</div>
  ))}
  {items.map((item) => (
    /* foo < > < } */ <div id={color}>color</div>
  ))}
</div>`,
			want: want{
				frontmatter: []string{"", "const items = ['red', 'yellow', 'blue'];"},
				code: `<div>
  ${items.map((item) => (
    // foo < > < }
$$render` + "`" + `<div${$$addAttribute(color, "id")}>color</div>` + "`" + `
  ))}
  ${items.map((item) => (
    /* foo < > < } */$$render` + "`" + `<div${$$addAttribute(color, "id")}>color</div>` + "`" + `
  ))}
</div>`,
			},
		},
		{
			name: "expressions with multiple curly braces",
			source: `
<div>
{
	() => {
		let generate = (input) => {
			let a = () => { return; };
			let b = () => { return; };
			let c = () => { return; };
		};
	}
}
</div>`,
			want: want{
				code: `<div>
${
	() => {
		let generate = (input) => {
			let a = () => { return; };
			let b = () => { return; };
			let c = () => { return; };
		};
	}
}
</div>`,
			},
		},
		{
			name: "slots (basic)",
			source: `---
import Component from "test";
---
<Component>
	<div>Default</div>
	<div slot="named">Named</div>
</Component>`,
			want: want{
				frontmatter: []string{`import Component from "test";`},
				metadata:    metadata{modules: []string{`{ module: $$module1, specifier: 'test', assert: {} }`}},
				code:        `${$$renderComponent($$result,'Component',Component,{},{"default": () => $$render` + "`" + `<div>Default</div>` + "`" + `,"named": () => $$render` + "`" + `<div>Named</div>` + "`" + `,})}`,
			},
		},
		{
			name: "slots (no comments)",
			source: `---
import Component from 'test';
---
<Component>
	<div>Default</div>
	<!-- A comment! -->
	<div slot="named">Named</div>
</Component>`,
			want: want{
				frontmatter: []string{`import Component from 'test';`},
				metadata:    metadata{modules: []string{`{ module: $$module1, specifier: 'test', assert: {} }`}},
				code:        `${$$renderComponent($$result,'Component',Component,{},{"default": () => $$render` + "`" + `<div>Default</div>` + "`" + `,"named": () => $$render` + "`" + `<div>Named</div>` + "`" + `,})}`,
			},
		},
		{
			name: "slots (expression)",
			source: `
<Component {data}>
	{items.map(item => <div>{item}</div>)}
</Component>`,
			want: want{
				code: `${$$renderComponent($$result,'Component',Component,{"data":(data)},{"default": () => $$render` + BACKTICK + `${items.map(item => $$render` + BACKTICK + `<div>${item}</div>` + BACKTICK + `)}` + BACKTICK + `,})}`,
			},
		},
		{
			name: "head expression",
			source: `---
const name = "world";
---
<html>
  <head>
    <title>Hello {name}</title>
  </head>
  <body>
    <div></div>
  </body>
</html>`,
			want: want{
				frontmatter: []string{``, `const name = "world";`},
				code: `<html>
  <head>
    <title>Hello ${name}</title>
  ` + RENDER_HEAD_RESULT + `</head>
  <body>
    <div></div>
  </body></html>
  `,
			},
		},
		{
			name: "styles (no frontmatter)",
			source: `<style>
		  .title {
		    font-family: fantasy;
		    font-size: 28px;
		  }

		  .body {
		    font-size: 1em;
		  }
		</style>

		<h1 class="title">Page Title</h1>
		<p class="body">I’m a page</p>`,
			want: want{
				styles: []string{"{props:{\"data-astro-id\":\"DPOHFLYM\"},children:`.title.astro-DPOHFLYM{font-family:fantasy;font-size:28px}.body.astro-DPOHFLYM{font-size:1em}`}"},
				code: "\n\n\t\t" + `<h1 class="title astro-DPOHFLYM">Page Title</h1>
		<p class="body astro-DPOHFLYM">I’m a page</p>`,
			},
		},
		{
			name: "html5 boilerplate",
			source: `<!doctype html>

<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">

  <title>A Basic HTML5 Template</title>
  <meta name="description" content="A simple HTML5 Template for new projects.">
  <meta name="author" content="SitePoint">

  <meta property="og:title" content="A Basic HTML5 Template">
  <meta property="og:type" content="website">
  <meta property="og:url" content="https://www.sitepoint.com/a-basic-html5-template/">
  <meta property="og:description" content="A simple HTML5 Template for new projects.">
  <meta property="og:image" content="image.png">

  <link rel="icon" href="/favicon.ico">
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">

  <link rel="stylesheet" href="css/styles.css?v=1.0">

</head>

<body>
  <!-- your content here... -->
  <script is:inline src="js/scripts.js"></script>
  </body>
</html>`,
			want: want{
				code: `<!DOCTYPE html><html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">

  <title>A Basic HTML5 Template</title>
  <meta name="description" content="A simple HTML5 Template for new projects.">
  <meta name="author" content="SitePoint">

  <meta property="og:title" content="A Basic HTML5 Template">
  <meta property="og:type" content="website">
  <meta property="og:url" content="https://www.sitepoint.com/a-basic-html5-template/">
  <meta property="og:description" content="A simple HTML5 Template for new projects.">
  <meta property="og:image" content="image.png">

  <link rel="icon" href="/favicon.ico">
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">

  <link rel="stylesheet" href="css/styles.css?v=1.0">

` + RENDER_HEAD_RESULT + `</head>

<body>
  <!-- your content here... -->
  <script src="js/scripts.js"></script>
  </body>
</html>`,
			},
		},
		{
			name: "React framework example",
			source: `---
// Component Imports
import Counter from '../components/Counter.jsx'
const someProps = {
  count: 0,
}

// Full Astro Component Syntax:
// https://docs.astro.build/core-concepts/astro-components/
---
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta
      name="viewport"
      content="width=device-width"
    />
    <link rel="icon" type="image/x-icon" href="/favicon.ico" />
    <style>
      :global(:root) {
        font-family: system-ui;
        padding: 2em 0;
      }
      :global(.counter) {
        display: grid;
        grid-template-columns: repeat(3, minmax(0, 1fr));
        place-items: center;
        font-size: 2em;
        margin-top: 2em;
      }
      :global(.children) {
        display: grid;
        place-items: center;
        margin-bottom: 2em;
      }
    </style>
  </head>
  <body>
    <main>
      <Counter {...someProps} client:visible>
        <h1>Hello React!</h1>
      </Counter>
    </main>
  </body>
</html>`,
			want: want{
				frontmatter: []string{`// Component Imports
import Counter from '../components/Counter.jsx'`,
					`const someProps = {
  count: 0,
}

// Full Astro Component Syntax:
// https://docs.astro.build/core-concepts/astro-components/`},
				styles: []string{fmt.Sprintf(`{props:{"data-astro-id":"HMNNHVCQ"},children:%s:root{font-family:system-ui;padding:2em 0}.counter{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));place-items:center;font-size:2em;margin-top:2em}.children{display:grid;place-items:center;margin-bottom:2em}%s}`, BACKTICK, BACKTICK)},
				metadata: metadata{
					modules:             []string{`{ module: $$module1, specifier: '../components/Counter.jsx', assert: {} }`},
					hydratedComponents:  []string{`Counter`},
					hydrationDirectives: []string{"visible"},
				},
				code: `<html lang="en" class="astro-HMNNHVCQ">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width">
    <link rel="icon" type="image/x-icon" href="/favicon.ico">
  
  ` + RENDER_HEAD_RESULT + `</head>
  <body>
    <main class="astro-HMNNHVCQ">
      ${$$renderComponent($$result,'Counter',Counter,{...(someProps),"client:visible":true,"client:component-hydration":"visible","client:component-path":($$metadata.getPath(Counter)),"client:component-export":($$metadata.getExport(Counter)),"class":"astro-HMNNHVCQ"},{"default": () => $$render` + "`" + `<h1 class="astro-HMNNHVCQ">Hello React!</h1>` + "`" + `,})}
    </main>
  </body></html>
  `,
			},
		},
		{
			name: "script in <head>",
			source: `---
import Widget from '../components/Widget.astro';
import Widget2 from '../components/Widget2.astro';
---
<html lang="en">
  <head>
    <script type="module" src="/regular_script.js"></script>
  </head>`,
			want: want{
				frontmatter: []string{`import Widget from '../components/Widget.astro';
import Widget2 from '../components/Widget2.astro';`},
				styles: []string{},
				metadata: metadata{
					modules: []string{
						`{ module: $$module1, specifier: '../components/Widget.astro', assert: {} }`,
						`{ module: $$module2, specifier: '../components/Widget2.astro', assert: {} }`},
				},
				code: `<html lang="en">
  <head>
    <script type="module" src="/regular_script.js"></script>
  ` + RENDER_HEAD_RESULT + `</head></html>`,
			},
		},
		{
			name: "script hoist with frontmatter",
			source: `---
---
<script type="module" hoist>console.log("Hello");</script>`,
			want: want{
				frontmatter: []string{""},
				styles:      []string{},
				scripts:     []string{fmt.Sprintf(`{props:{"type":"module","hoist":true},children:%sconsole.log("Hello");%s}`, BACKTICK, BACKTICK)},
				metadata:    metadata{hoisted: []string{fmt.Sprintf(`{ type: 'inline', value: %sconsole.log("Hello");%s }`, BACKTICK, BACKTICK)}},
				code:        ``,
			},
		},
		{
			name: "script hoist remote",
			source: `---
---
<script type="module" hoist src="url" />`,
			want: want{
				frontmatter: []string{"\n"},
				styles:      []string{},
				scripts:     []string{`{props:{"type":"module","hoist":true,"src":"url"}}`},
				metadata:    metadata{hoisted: []string{`{ type: 'remote', src: 'url' }`}},
				code:        "",
			},
		},
		{
			name: "script hoist without frontmatter",
			source: `
							<main>
								<script type="module" hoist>console.log("Hello");</script>
							`,
			want: want{
				styles:   []string{},
				scripts:  []string{"{props:{\"type\":\"module\",\"hoist\":true},children:`console.log(\"Hello\");`}"},
				metadata: metadata{hoisted: []string{fmt.Sprintf(`{ type: 'inline', value: %sconsole.log("Hello");%s }`, BACKTICK, BACKTICK)}},
				code: `<main>

</main>`,
			},
		},
		{
			name:   "script inline",
			source: `<main><script is:inline type="module">console.log("Hello");</script>`,
			want: want{
				code: `<main><script type="module">console.log("Hello");</script></main>`,
			},
		},
		{
			name:   "script define:vars",
			source: `<main><script define:vars={{ value: 0 }} type="module">console.log(value);</script>`,
			want: want{
				code: fmt.Sprintf(`<main><script type="module">${%s({ value: 0 })}console.log(value);</script></main>`, DEFINE_SCRIPT_VARS),
			},
		},
		{
			name:   "text after title expression",
			source: `<title>a {expr} b</title>`,
			want: want{
				code: `<title>a ${expr} b</title>`,
			},
		},
		{
			name:   "text after title expressions",
			source: `<title>a {expr} b {expr} c</title>`,
			want: want{
				code: `<title>a ${expr} b ${expr} c</title>`,
			},
		},
		{
			name: "slots (dynamic name)",
			source: `---
		import Component from 'test';
		const name = 'named';
		---
		<Component>
			<div slot={name}>Named</div>
		</Component>`,
			want: want{
				frontmatter: []string{`import Component from 'test';`, `const name = 'named';`},
				styles:      []string{},
				metadata:    metadata{modules: []string{`{ module: $$module1, specifier: 'test', assert: {} }`}},
				code:        `${$$renderComponent($$result,'Component',Component,{},{[name]: () => $$render` + "`" + `<div>Named</div>` + "`" + `,})}`,
			},
		},
		{
			name:   "condition expressions at the top-level",
			source: `{cond && <span></span>}{cond && <strong></strong>}`,
			want: want{
				code: "${cond && $$render`<span></span>`}${cond && $$render`<strong></strong>`}",
			},
		},
		{
			name:   "condition expressions at the top-level with head content",
			source: `{cond && <meta charset=utf8>}{cond && <title>My title</title>}`,
			want: want{
				code: "${cond && $$render`<meta charset=\"utf8\">`}${cond && $$render`<title>My title</title>`}",
			},
		},
		{
			name: "custom elements",
			source: `---
import 'test';
---
<my-element></my-element>`,
			want: want{
				frontmatter: []string{`import 'test';`},
				styles:      []string{},
				metadata:    metadata{modules: []string{`{ module: $$module1, specifier: 'test', assert: {} }`}},
				code:        `${$$renderComponent($$result,'my-element','my-element',{})}`,
			},
		},
		{
			name: "gets all potential hydrated components",
			source: `---
import One from 'one';
import Two from 'two';
import 'custom-element';
const name = 'world';
---
<One client:load />
<Two client:load />
<my-element client:load />
`,
			want: want{
				frontmatter: []string{`import One from 'one';
import Two from 'two';
import 'custom-element';`,
					`const name = 'world';`},
				metadata: metadata{
					modules: []string{
						`{ module: $$module1, specifier: 'one', assert: {} }`,
						`{ module: $$module2, specifier: 'two', assert: {} }`,
						`{ module: $$module3, specifier: 'custom-element', assert: {} }`,
					},
					hydratedComponents:  []string{"'my-element'", "Two", "One"},
					hydrationDirectives: []string{"load"},
				},
				code: `${$$renderComponent($$result,'One',One,{"client:load":true,"client:component-hydration":"load","client:component-path":($$metadata.getPath(One)),"client:component-export":($$metadata.getExport(One))})}
${$$renderComponent($$result,'Two',Two,{"client:load":true,"client:component-hydration":"load","client:component-path":($$metadata.getPath(Two)),"client:component-export":($$metadata.getExport(Two))})}
${$$renderComponent($$result,'my-element','my-element',{"client:load":true,"client:component-hydration":"load","client:component-path":($$metadata.getPath('my-element')),"client:component-export":($$metadata.getExport('my-element'))})}`,
			},
		},
		{
			name:   "Component siblings are siblings",
			source: `<BaseHead></BaseHead><link href="test">`,
			want: want{
				code: `${$$renderComponent($$result,'BaseHead',BaseHead,{})}<link href="test">`,
			},
		},
		{
			name:   "Self-closing components siblings are siblings",
			source: `<BaseHead /><link href="test">`,
			want: want{
				code: `${$$renderComponent($$result,'BaseHead',BaseHead,{})}<link href="test">`,
			},
		},
		{
			name:   "Self-closing script in head works",
			source: `<html><head><script is:inline /></head><html>`,
			want: want{
				code: `<html><head><script></script>` + RENDER_HEAD_RESULT + `</head></html>`,
			},
		},
		{
			name:   "Self-closing title",
			source: `<title />`,
			want: want{
				code: `<title></title>`,
			},
		},
		{
			name:   "Self-closing title II",
			source: `<html><head><title /></head><body></body></html>`,
			want: want{
				code: `<html><head><title></title>` + RENDER_HEAD_RESULT + `</head><body></body></html>`,
			},
		},
		{
			name:   "Self-closing components in head can have siblings",
			source: `<html><head><BaseHead /><link href="test"></head><html>`,
			want: want{
				code: `<html><head>${$$renderComponent($$result,'BaseHead',BaseHead,{})}<link href="test">` + RENDER_HEAD_RESULT + `</head></html>`,
			},
		},
		{
			name:   "Self-closing formatting elements",
			source: `<div id="1"><div id="2"><div id="3"><i/><i/><i/></div></div></div>`,
			want: want{
				code: `<div id="1"><div id="2"><div id="3"><i></i><i></i><i></i></div></div></div>`,
			},
		},
		{
			name: "Self-closing formatting elements 2",
			source: `<body>
  <div id="1"><div id="2"><div id="3"><i id="a" /></div></div></div>
  <div id="4"><div id="5"><div id="6"><i id="b" /></div></div></div>
  <div id="7"><div id="8"><div id="9"><i id="c" /></div></div></div>
</body>`,
			want: want{
				code: `<body>
  <div id="1"><div id="2"><div id="3"><i id="a"></i></div></div></div>
  <div id="4"><div id="5"><div id="6"><i id="b"></i></div></div></div>
  <div id="7"><div id="8"><div id="9"><i id="c"></i></div></div></div>
</body>`,
			},
		},
		{
			name: "Nested HTML in expressions, wrapped in parens",
			source: `---
const image = './penguin.png';
const canonicalURL = new URL('http://example.com');
---
{image && (<meta property="og:image" content={new URL(image, canonicalURL)}>)}`,
			want: want{
				frontmatter: []string{"", `const image = './penguin.png';
const canonicalURL = new URL('http://example.com');`},
				styles: []string{},
				code:   "${image && ($$render`<meta property=\"og:image\"${$$addAttribute(new URL(image, canonicalURL), \"content\")}>`)}",
			},
		},
		{
			name: "Use of interfaces within frontmatter",
			source: `---
interface MarkdownFrontmatter {
	date: number;
	image: string;
	author: string;
}
let allPosts = Astro.fetchContent<MarkdownFrontmatter>('./post/*.md');
---
<div>testing</div>`,
			want: want{
				frontmatter: []string{"", `interface MarkdownFrontmatter {
	date: number;
	image: string;
	author: string;
}
let allPosts = Astro.fetchContent<MarkdownFrontmatter>('./post/*.md');`},
				styles: []string{},
				code:   "<div>testing</div>",
			},
		},
		{
			name: "Component names A-Z",
			source: `---
import AComponent from '../components/AComponent.jsx';
import ZComponent from '../components/ZComponent.jsx';
---

<body>
  <AComponent />
  <ZComponent />
</body>`,
			want: want{
				frontmatter: []string{
					`import AComponent from '../components/AComponent.jsx';
import ZComponent from '../components/ZComponent.jsx';`},
				metadata: metadata{
					modules: []string{
						`{ module: $$module1, specifier: '../components/AComponent.jsx', assert: {} }`,
						`{ module: $$module2, specifier: '../components/ZComponent.jsx', assert: {} }`,
					},
				},
				code: `<body>
  ${` + RENDER_COMPONENT + `($$result,'AComponent',AComponent,{})}
  ${` + RENDER_COMPONENT + `($$result,'ZComponent',ZComponent,{})}
</body>`,
			},
		},
		{
			name: "Parser can handle files > 4096 chars",
			source: `<html><body>` + longRandomString + `<img
  width="1600"
  height="1131"
  class="img"
  src="https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=1200&q=75"
  srcSet="https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=1200&q=75 800w,https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=1200&q=75 1200w,https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=1600&q=75 1600w,https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=2400&q=75 2400w"
  sizes="(max-width: 800px) 800px, (max-width: 1200px) 1200px, (max-width: 1600px) 1600px, (max-width: 2400px) 2400px, 1200px"
>`,
			want: want{
				code: `<html><body>` + longRandomString + `<img width="1600" height="1131" class="img" src="https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=1200&q=75" srcSet="https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=1200&q=75 800w,https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=1200&q=75 1200w,https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=1600&q=75 1600w,https://images.unsplash.com/photo-1469854523086-cc02fe5d8800?w=2400&q=75 2400w" sizes="(max-width: 800px) 800px, (max-width: 1200px) 1200px, (max-width: 1600px) 1600px, (max-width: 2400px) 2400px, 1200px"></body></html>`,
			},
		},
		{
			name:   "SVG styles",
			source: `<svg><style>path { fill: red; }</style></svg>`,
			want: want{
				code: `<svg><style>path { fill: red; }</style></svg>`,
			},
		},
		{
			name: "svg expressions",
			source: `---
const title = 'icon';
---
<svg>{title ?? null}</svg>`,
			want: want{
				frontmatter: []string{"", "const title = 'icon';"},
				code:        `<svg>${title ?? null}</svg>`,
			},
		},
		{
			name: "advanced svg expression",
			source: `---
const title = 'icon';
---
<svg>{title ? <title>{title}</title> : null}</svg>`,
			want: want{
				frontmatter: []string{"", "const title = 'icon';"},
				code:        `<svg>${title ? $$render` + BACKTICK + `<title>${title}</title>` + BACKTICK + ` : null}</svg>`,
			},
		},
		{
			name:   "Empty script",
			source: `<script hoist></script>`,
			want: want{
				scripts: []string{`{props:{"hoist":true}}`},
				code:    ``,
			},
		},
		{
			name:   "Empty style",
			source: `<style define:vars={{ color: "Gainsboro" }}></style>`,
			want: want{
				styles: []string{`{props:{"define:vars":({ color: "Gainsboro" }),"data-astro-id":"7HAAVZPE"}}`},
				code:   ``,
			},
		},
		{
			name: "No extra script tag",
			source: `<!-- Global Metadata -->
<meta charset="utf-8">
<meta name="viewport" content="width=device-width">

<link rel="icon" type="image/svg+xml" href="/favicon.svg" />
<link rel="alternate icon" type="image/x-icon" href="/favicon.ico" />

<link rel="sitemap" href="/sitemap.xml"/>

<!-- Global CSS -->
<link rel="stylesheet" href="/theme.css" />
<link rel="stylesheet" href="/code.css" />
<link rel="stylesheet" href="/index.css" />

<!-- Preload Fonts -->
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:ital@0;1&display=swap" rel="stylesheet">

<!-- Scrollable a11y code helper -->
<script type="module" src="/make-scrollable-code-focusable.js" />

<!-- This is intentionally inlined to avoid FOUC -->
<script is:inline>
  const root = document.documentElement;
  const theme = localStorage.getItem('theme');
  if (theme === 'dark' || (!theme) && window.matchMedia('(prefers-color-scheme: dark)').matches) {
    root.classList.add('theme-dark');
  } else {
    root.classList.remove('theme-dark');
  }
</script>

<!-- Global site tag (gtag.js) - Google Analytics -->
<!-- <script async src="https://www.googletagmanager.com/gtag/js?id=G-TEL60V1WM9"></script>
<script>
  window.dataLayer = window.dataLayer || [];
  function gtag(){dataLayer.push(arguments);}
  gtag('js', new Date());
  gtag('config', 'G-TEL60V1WM9');
</script> -->`,
			want: want{
				code: `<!-- Global Metadata --><meta charset="utf-8">
<meta name="viewport" content="width=device-width">

<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="alternate icon" type="image/x-icon" href="/favicon.ico">

<link rel="sitemap" href="/sitemap.xml">

<!-- Global CSS -->
<link rel="stylesheet" href="/theme.css">
<link rel="stylesheet" href="/code.css">
<link rel="stylesheet" href="/index.css">

<!-- Preload Fonts -->
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:ital@0;1&display=swap" rel="stylesheet">

<!-- Scrollable a11y code helper -->
<script type="module" src="/make-scrollable-code-focusable.js"></script>

<!-- This is intentionally inlined to avoid FOUC -->
<script>
  const root = document.documentElement;
  const theme = localStorage.getItem('theme');
  if (theme === 'dark' || (!theme) && window.matchMedia('(prefers-color-scheme: dark)').matches) {
    root.classList.add('theme-dark');
  } else {
    root.classList.remove('theme-dark');
  }
</script>

<!-- Global site tag (gtag.js) - Google Analytics -->
<!-- <script async src="https://www.googletagmanager.com/gtag/js?id=G-TEL60V1WM9"></script>
<script>
  window.dataLayer = window.dataLayer || [];
  function gtag(){dataLayer.push(arguments);}
  gtag('js', new Date());
  gtag('config', 'G-TEL60V1WM9');
</script> -->`,
			},
		},
		{
			name: "All components",
			source: `
---
import { Container, Col, Row } from 'react-bootstrap';
---
<Container>
    <Row>
        <Col>
            <h1>Hi!</h1>
        </Col>
    </Row>
</Container>
`,
			want: want{
				frontmatter: []string{`import { Container, Col, Row } from 'react-bootstrap';`},
				metadata:    metadata{modules: []string{`{ module: $$module1, specifier: 'react-bootstrap', assert: {} }`}},
				code:        "${$$renderComponent($$result,'Container',Container,{},{\"default\": () => $$render`${$$renderComponent($$result,'Row',Row,{},{\"default\": () => $$render`${$$renderComponent($$result,'Col',Col,{},{\"default\": () => $$render`<h1>Hi!</h1>`,})}`,})}`,})}",
			},
		},
		{
			name: "Mixed style siblings",
			source: `<head>
	<style is:global>div { color: red }</style>
	<style is:scoped>div { color: green }</style>
	<style>div { color: blue }</style>
</head>
<div />`,
			want: want{
				styles: []string{
					"{props:{\"data-astro-id\":\"LASNTLJA\"},children:`div.astro-LASNTLJA{color:blue}`}",
					"{props:{\"is:scoped\":true,\"data-astro-id\":\"LASNTLJA\"},children:`div.astro-LASNTLJA{color:green}`}",
					"{props:{\"is:global\":true},children:`div { color: red }`}",
				},
				code: "<head>\n\n\n\n\n\n\n" + RENDER_HEAD_RESULT + "</head>\n<div class=\"astro-LASNTLJA\"></div>",
			},
		},
		{
			name:   "Fragment",
			source: `<body><Fragment><div>Default</div><div>Named</div></Fragment></body>`,
			want: want{
				code: `<body>${$$renderComponent($$result,'Fragment',Fragment,{},{"default": () => $$render` + BACKTICK + `<div>Default</div><div>Named</div>` + BACKTICK + `,})}</body>`,
			},
		},
		{
			name:   "Fragment shorthand",
			source: `<body><><div>Default</div><div>Named</div></></body>`,
			want: want{
				code: `<body>${$$renderComponent($$result,'Fragment',Fragment,{},{"default": () => $$render` + BACKTICK + `<div>Default</div><div>Named</div>` + BACKTICK + `,})}</body>`,
			},
		},
		{
			name:   "Fragment shorthand only",
			source: `<>Hello</>`,
			want: want{
				code: `${$$renderComponent($$result,'Fragment',Fragment,{},{"default": () => $$render` + BACKTICK + `Hello` + BACKTICK + `,})}`,
			},
		},
		{
			name:   "Fragment literal only",
			source: `<Fragment>world</Fragment>`,
			want: want{
				code: `${$$renderComponent($$result,'Fragment',Fragment,{},{"default": () => $$render` + BACKTICK + `world` + BACKTICK + `,})}`,
			},
		},
		{
			name:   "Fragment slotted",
			source: `<body><Component><><div>Default</div><div>Named</div></></Component></body>`,
			want: want{
				code: `<body>${$$renderComponent($$result,'Component',Component,{},{"default": () => $$render` + BACKTICK + `${$$renderComponent($$result,'Fragment',Fragment,{},{"default": () => $$render` + BACKTICK + `<div>Default</div><div>Named</div>` + BACKTICK + `,})}` + BACKTICK + `,})}</body>`,
			},
		},
		{
			name:   "Fragment slotted with name",
			source: `<body><Component><Fragment slot=named><div>Default</div><div>Named</div></Fragment></Component></body>`,
			want: want{
				code: `<body>${$$renderComponent($$result,'Component',Component,{},{"named": () => $$render` + BACKTICK + `${$$renderComponent($$result,'Fragment',Fragment,{"slot":"named"},{"default": () => $$render` + BACKTICK + `<div>Default</div><div>Named</div>` + BACKTICK + `,})}` + BACKTICK + `,})}</body>`,
			},
		},
		{
			name:   "Preserve slots inside custom-element",
			source: `<body><my-element><div slot=name>Name</div><div>Default</div></my-element></body>`,
			want: want{
				code: `<body>${$$renderComponent($$result,'my-element','my-element',{},{"default": () => $$render` + BACKTICK + `<div slot="name">Name</div><div>Default</div>` + BACKTICK + `,})}</body>`,
			},
		},
		{
			name:   "Preserve namespaces",
			source: `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><rect xlink:href="#id"></svg>`,
			want: want{
				code: `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><rect xlink:href="#id"></rect></svg>`,
			},
		},
		{
			name: "import.meta.env",
			source: fmt.Sprintf(`---
import Header from '../../components/Header.jsx'
import Footer from '../../components/Footer.astro'
import ProductPageContent from '../../components/ProductPageContent.jsx';

export async function getStaticPaths() {
  let products = await fetch(%s${import.meta.env.PUBLIC_NETLIFY_URL}/.netlify/functions/get-product-list%s)
    .then(res => res.json()).then((response) => {
      console.log('--- built product pages ---')
      return response.products.edges
    });

  return products.map((p, i) => {
    return {
      params: {pid: p.node.handle},
      props: {product: p},
    };
  });
}

const { product } = Astro.props;
---

<!doctype html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Shoperoni | Buy {product.node.title}</title>

  <link rel="icon" type="image/svg+xml" href="/favicon.svg">
  <link rel="stylesheet" href="/style/global.css">
</head>
<body>
  <Header />
  <div class="product-page">
    <article>
      <ProductPageContent client:visible product={product.node} />
    </article>
  </div>
  <Footer />
</body>
</html>`, BACKTICK, BACKTICK),
			want: want{
				code: `<!DOCTYPE html><html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Shoperoni | Buy ${product.node.title}</title>

  <link rel="icon" type="image/svg+xml" href="/favicon.svg">
  <link rel="stylesheet" href="/style/global.css">
` + RENDER_HEAD_RESULT + `</head>
<body>
  ${$$renderComponent($$result,'Header',Header,{})}
  <div class="product-page">
    <article>
      ${$$renderComponent($$result,'ProductPageContent',ProductPageContent,{"client:visible":true,"product":(product.node),"client:component-hydration":"visible","client:component-path":($$metadata.getPath(ProductPageContent)),"client:component-export":($$metadata.getExport(ProductPageContent))})}
    </article>
  </div>
  ${$$renderComponent($$result,'Footer',Footer,{})}
</body></html>
`,
				frontmatter: []string{
					`import Header from '../../components/Header.jsx'
import Footer from '../../components/Footer.astro'
import ProductPageContent from '../../components/ProductPageContent.jsx';`,
					"const { product } = Astro.props;",
				},
				getStaticPaths: fmt.Sprintf(`export async function getStaticPaths() {
  let products = await fetch(%s${import.meta.env.PUBLIC_NETLIFY_URL}/.netlify/functions/get-product-list%s)
    .then(res => res.json()).then((response) => {
      console.log('--- built product pages ---')
      return response.products.edges
    });

  return products.map((p, i) => {
    return {
      params: {pid: p.node.handle},
      props: {product: p},
    };
  });
}`, BACKTICK, BACKTICK),
				skipHoist: true,
				metadata: metadata{
					modules: []string{`{ module: $$module1, specifier: '../../components/Header.jsx', assert: {} }`,
						`{ module: $$module2, specifier: '../../components/Footer.astro', assert: {} }`,
						`{ module: $$module3, specifier: '../../components/ProductPageContent.jsx', assert: {} }`,
					},
					hydratedComponents:  []string{`ProductPageContent`},
					hydrationDirectives: []string{"visible"},
				},
			},
		},
		{
			name:   "doctype",
			source: `<!DOCTYPE html><div/>`,
			want: want{
				code: `<!DOCTYPE html><div></div>`,
			},
		},
		{
			name: "select option expression",
			source: `---
const value = 'test';
---
<select><option>{value}</option></select>`,
			want: want{
				frontmatter: []string{"", "const value = 'test';"},
				code:        `<select><option>${value}</option></select>`,
			},
		},
		{
			name: "select nested option",
			source: `---
const value = 'test';
---
<select>{value && <option>{value}</option>}</select>`,
			want: want{
				frontmatter: []string{"", "const value = 'test';"},
				code:        `<select>${value && $$render` + BACKTICK + `<option>${value}</option>` + BACKTICK + `}</select>`,
			},
		},
		{
			name:   "select map expression",
			source: `<select>{[1, 2, 3].map(num => <option>{num}</option>)}</select><div>Hello world!</div>`,
			want: want{
				code: `<select>${[1, 2, 3].map(num => $$render` + BACKTICK + `<option>${num}</option>` + BACKTICK + `)}</select><div>Hello world!</div>`,
			},
		},
		{
			name: "textarea",
			source: `---
const value = 'test';
---
<textarea>{value}</textarea>`,
			want: want{
				frontmatter: []string{"", "const value = 'test';"},
				code:        `<textarea>${value}</textarea>`,
			},
		},
		{
			name:   "textarea inside expression",
			source: `{bool && <textarea>{value}</textarea>} {!bool && <input>}`,
			want: want{
				code: `${bool && $$render` + BACKTICK + `<textarea>${value}</textarea>` + BACKTICK + `} ${!bool && $$render` + BACKTICK + `<input>` + BACKTICK + `}`,
			},
		},
		{
			name: "table expressions (no implicit tbody)",
			source: `---
const items = ["Dog", "Cat", "Platipus"];
---
<table>{items.map(item => (<tr><td>{item}</td></tr>))}</table>`,
			want: want{
				frontmatter: []string{"", `const items = ["Dog", "Cat", "Platipus"];`},
				code:        `<table>${items.map(item => ($$render` + BACKTICK + `<tr><td>${item}</td></tr>` + BACKTICK + `))}</table>`,
			},
		},
		{
			name: "tbody expressions",
			source: `---
const items = ["Dog", "Cat", "Platipus"];
---
<table><tr><td>Name</td></tr>{items.map(item => (<tr><td>{item}</td></tr>))}</table>`,
			want: want{
				frontmatter: []string{"", `const items = ["Dog", "Cat", "Platipus"];`},
				code:        `<table><tr><td>Name</td></tr>${items.map(item => ($$render` + BACKTICK + `<tr><td>${item}</td></tr>` + BACKTICK + `))}</table>`,
			},
		},
		{
			name: "tbody expressions 2",
			source: `---
const items = ["Dog", "Cat", "Platipus"];
---
<table><tr><td>Name</td></tr>{items.map(item => (<tr><td>{item}</td><td>{item + 's'}</td></tr>))}</table>`,
			want: want{
				frontmatter: []string{"", `const items = ["Dog", "Cat", "Platipus"];`},
				code:        `<table><tr><td>Name</td></tr>${items.map(item => ($$render` + BACKTICK + `<tr><td>${item}</td><td>${item + 's'}</td></tr>` + BACKTICK + `))}</table>`,
			},
		},
		{
			name:   "td expressions",
			source: `<table><tr><td><h2>Row 1</h2></td><td>{title}</td></tr></table>`,
			want: want{
				code: `<table><tr><td><h2>Row 1</h2></td><td>${title}</td></tr></table>`,
			},
		},
		{
			name:   "th expressions",
			source: `<table><thead><tr><th>{title}</th></tr></thead></table>`,
			want: want{
				code: `<table><thead><tr><th>${title}</th></tr></thead></table>`,
			},
		},
		{
			name:   "tr only",
			source: `<tr><td>col 1</td><td>col 2</td><td>{foo}</td></tr>`,
			want: want{
				code: `<tr><td>col 1</td><td>col 2</td><td>${foo}</td></tr>`,
			},
		},
		{
			name:   "caption only",
			source: `<caption>Hello world!</caption>`,
			want: want{
				code: `<caption>Hello world!</caption>`,
			},
		},
		{
			name:   "anchor expressions",
			source: `<a>{expr}</a>`,
			want: want{
				code: `<a>${expr}</a>`,
			},
		},
		{
			name:   "anchor inside expression",
			source: `{true && <a>expr</a>}`,
			want: want{
				code: `${true && $$render` + BACKTICK + `<a>expr</a>` + BACKTICK + `}`,
			},
		},
		{
			name:   "anchor content",
			source: `<a><div><h3></h3><ul><li>{expr}</li></ul></div></a>`,
			want: want{
				code: `<a><div><h3></h3><ul><li>${expr}</li></ul></div></a>`,
			},
		},
		{
			name:   "small expression",
			source: `<div><small>{a}</small>{data.map(a => <Component value={a} />)}</div>`,
			want: want{
				code: `<div><small>${a}</small>${data.map(a => $$render` + BACKTICK + `${$$renderComponent($$result,'Component',Component,{"value":(a)})}` + BACKTICK + `)}</div>`,
			},
		},
		{
			name:   "division inside expression",
			source: `<div>{16 / 4}</div>`,
			want: want{
				code: `<div>${16 / 4}</div>`,
			},
		},
		{
			name:   "escaped entity",
			source: `<img alt="A person saying &#x22;hello&#x22;">`,
			want: want{
				code: `<img alt="A person saying &quot;hello&quot;">`,
			},
		},
		{
			name:   "textarea in form",
			source: `<html><Component><form><textarea></textarea></form></Component></html>`,
			want: want{
				code: `<html>${$$renderComponent($$result,'Component',Component,{},{"default": () => $$render` + BACKTICK + `<form><textarea></textarea></form>` + BACKTICK + `,})}</html>`,
			},
		},
		{
			name:   "slot inside of Base",
			source: `<Base title="Home"><div>Hello</div></Base>`,
			want: want{
				code: `${$$renderComponent($$result,'Base',Base,{"title":"Home"},{"default": () => $$render` + BACKTICK + `<div>Hello</div>` + BACKTICK + `,})}`,
			},
		},
		{
			name:   "user-defined `implicit` is printed",
			source: `<html implicit></html>`,
			want: want{
				code: `<html implicit></html>`,
			},
		},
		{
			name: "css comment doesn’t produce semicolon",
			source: `<style>/* comment */.container {
    padding: 2rem;
	}
</style><div class="container">My Text</div>`,

			want: want{
				styles: []string{fmt.Sprintf(`{props:{"data-astro-id":"SJ3WYE6H"},children:%s.container.astro-SJ3WYE6H{padding:2rem}%s}`, BACKTICK, BACKTICK)},
				code:   `<div class="container astro-SJ3WYE6H">My Text</div>`,
			},
		},
		{
			name: "sibling expressions",
			source: `<html><body>
  <table>
  {true ? (<tr><td>Row 1</td></tr>) : null}
  {true ? (<tr><td>Row 2</td></tr>) : null}
  {true ? (<tr><td>Row 3</td></tr>) : null}
  </table>
</body>`,
			want: want{
				code: fmt.Sprintf(`<html><body>
  <table>
  ${true ? ($$render%s<tr><td>Row 1</td></tr>%s) : null}
  ${true ? ($$render%s<tr><td>Row 2</td></tr>%s) : null}
  ${true ? ($$render%s<tr><td>Row 3</td></tr>%s) : null}

</table></body></html>`, BACKTICK, BACKTICK, BACKTICK, BACKTICK, BACKTICK, BACKTICK),
			},
		},
		{
			name:   "XElement",
			source: `<XElement {...attrs}></XElement>{onLoadString ? <script data-something></script> : null }`,
			want: want{
				code: fmt.Sprintf(`${$$renderComponent($$result,'XElement',XElement,{...(attrs)})}${onLoadString ? $$render%s<script data-something></script>%s : null }`, BACKTICK, BACKTICK),
			},
		},
		{
			name:   "Empty expression",
			source: "<body>({})</body>",
			want: want{
				code: `<body>(${(void 0)})</body>`,
			},
		},
		{
			name:   "Empty attribute expression",
			source: "<body attr={}></body>",
			want: want{
				code: `<body${$$addAttribute((void 0), "attr")}></body>`,
			},
		},
		{
			name:   "is:raw",
			source: "<article is:raw><% awesome %></article>",
			want: want{
				code: `<article><% awesome %></article>`,
			},
		},
		{
			name:   "Component is:raw",
			source: "<Component is:raw>{<% awesome %>}</Component>",
			want: want{
				code: "${$$renderComponent($$result,'Component',Component,{},{\"default\": () => $$render`{<% awesome %>}`,})}",
			},
		},
		{
			name:   "set:html",
			source: "<article set:html={content} />",
			want: want{
				code: `<article>${$$unescapeHTML(content)}</article>`,
			},
		},
		{
			name:   "set:text",
			source: "<article set:text={content} />",
			want: want{
				code: `<article>${content}</article>`,
			},
		},
		{
			name:   "set:html on Component",
			source: "<Component set:html={content} />",
			want: want{
				code: `${$$renderComponent($$result,'Component',Component,{},{"default": () => $$render` + "`${$$unescapeHTML(content)}`," + `})}`,
			},
		},
		{
			name:   "set:text on Component",
			source: "<Component set:text={content} />",
			want: want{
				code: `${$$renderComponent($$result,'Component',Component,{},{"default": () => $$render` + "`${content}`," + `})}`,
			},
		},
		{
			name:   "set:html on custom-element",
			source: "<custom-element set:html={content} />",
			want: want{
				code: `${$$renderComponent($$result,'custom-element','custom-element',{},{"default": () => $$render` + "`${$$unescapeHTML(content)}`," + `})}`,
			},
		},
		{
			name:   "set:text on custom-element",
			source: "<custom-element set:text={content} />",
			want: want{
				code: `${$$renderComponent($$result,'custom-element','custom-element',{},{"default": () => $$render` + "`${content}`," + `})}`,
			},
		},
		{
			name:   "set:html on self-closing tag",
			source: "<article set:html={content} />",
			want: want{
				code: `<article>${$$unescapeHTML(content)}</article>`,
			},
		},
		{
			name:   "set:html with other attributes",
			source: "<article set:html={content} cool=\"true\" />",
			want: want{
				code: `<article cool="true">${$$unescapeHTML(content)}</article>`,
			},
		},
		{
			name:   "set:html on empty tag",
			source: "<article set:html={content}></article>",
			want: want{
				code: `<article>${$$unescapeHTML(content)}</article>`,
			},
		},
		{
			// If both "set:*" directives are passed, we only respect the first one
			name:   "set:html and set:text",
			source: "<article set:html={content} set:text={content} />",
			want: want{
				code: `<article>${$$unescapeHTML(content)}</article>`,
			},
		},
		{
			name:   "set:html on tag with children",
			source: "<article set:html={content}>!!!</article>",
			want: want{
				code: `<article>${$$unescapeHTML(content)}</article>`,
			},
		},
		{
			name:   "set:html on tag with empty whitespace",
			source: "<article set:html={content}>   </article>",
			want: want{
				code: `<article>${$$unescapeHTML(content)}</article>`,
			},
		},
		{
			name:   "set:html on script",
			source: "<script set:html={content} />",
			want: want{
				code: `<script>${$$unescapeHTML(content)}</script>`,
			},
		},
		{
			name:   "set:html on style",
			source: "<style set:html={content} />",
			want: want{
				code: `<style>${$$unescapeHTML(content)}</style>`,
			},
		},
		{
			name:             "define:vars on style with StaticExpression turned on",
			source:           "<style>h1{color:green;}</style><style define:vars={{color:'green'}}>h1{color:var(--color)}</style><h1>testing</h1>",
			staticExtraction: true,
			want: want{
				code: `<h1 class="astro-VFS5OEMV">testing</h1>`,
				styles: []string{
					"{props:{\"define:vars\":({color:'green'}),\"data-astro-id\":\"VFS5OEMV\"},children:`h1.astro-VFS5OEMV{color:var(--color)}`}",
				},
			},
		},
		{
			name: "define:vars on script with StaticExpression turned on",
			// 1. An inline script with is:inline - right
			// 2. A hoisted script - wrong, shown up in scripts.add
			// 3. A define:vars module script - right
			// 4. A define:vars hoisted script - wrong, not inlined
			source:           `<script is:inline>var one = 'one';</script><script>var two = 'two';</script><script type="module">var three = foo;</script><script type="module" define:vars={{foo:'bar'}}>var four = foo;</script>`,
			staticExtraction: true,
			want: want{
				code: `<script>var one = 'one';</script><script type="module">var three = foo;</script><script type="module">${$$defineScriptVars({foo:'bar'})}var four = foo;</script>`,
				metadata: metadata{
					hoisted: []string{"{ type: 'inline', value: `var two = 'two';` }"},
				},
			},
		},
		{
			name:   "comments removed from attribute list",
			source: `<h1 {/* a comment */} value="1">Hello</h1>`,
			want: want{
				code: `<h1 value="1">Hello</h1>`,
			},
		},
		{
			name: "multiline comments removed from attribute list",
			source: `<h1 {/* 
				a comment over multiple lines
			*/} value="1">Hello</h1>`,
			want: want{
				code: `<h1 value="1">Hello</h1>`,
			},
		},
	}

	for _, tt := range tests {
		if tt.only {
			tests = make([]testcase, 0)
			tests = append(tests, tt)
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// transform output from source
			code := test_utils.Dedent(tt.source)

			doc, err := astro.Parse(strings.NewReader(code))

			if err != nil {
				t.Error(err)
			}

			hash := astro.HashFromSource(code)
			transform.ExtractStyles(doc)
			transform.Transform(doc, transform.TransformOptions{Scope: hash}) // note: we want to test Transform in context here, but more advanced cases could be tested separately
			result := PrintToJS(code, doc, 0, transform.TransformOptions{
				Scope:            "XXXX",
				Site:             "https://astro.build",
				InternalURL:      "http://localhost:3000/",
				ProjectRoot:      ".",
				StaticExtraction: tt.staticExtraction,
			})
			output := string(result.Output)

			toMatch := INTERNAL_IMPORTS
			if len(tt.want.frontmatter) > 0 {
				toMatch += test_utils.Dedent(tt.want.frontmatter[0])
			}
			// Fixes some tests where getStaticPaths appears in a different location
			if tt.want.skipHoist == true && len(tt.want.getStaticPaths) > 0 {
				toMatch += "\n\n"
				toMatch += strings.TrimSpace(test_utils.Dedent(tt.want.getStaticPaths)) + "\n"
			}
			moduleSpecRe := regexp.MustCompile(`specifier:\s*('[^']+'),\s*assert:\s*([^}]+\})`)
			if len(tt.want.metadata.modules) > 0 {
				toMatch += "\n\n"
				for i, m := range tt.want.metadata.modules {
					spec := moduleSpecRe.FindSubmatch([]byte(m)) // 0: full match, 1: submatch
					asrt := ""
					if string(spec[2]) != "{}" {
						asrt = " assert " + string(spec[2])
					}
					toMatch += fmt.Sprintf("import * as $$module%s from %s%s;\n", strconv.Itoa(i+1), string(spec[1]), asrt)
				}
			}
			// build metadata object from provided strings
			metadata := "{ "
			// metadata.modules
			metadata += "modules: ["
			if len(tt.want.metadata.modules) > 0 {
				for i, m := range tt.want.metadata.modules {
					if i > 0 {
						metadata += ", "
					}
					metadata += m
				}
			}
			metadata += "]"
			// metadata.hydratedComponents
			metadata += ", hydratedComponents: ["
			if len(tt.want.metadata.hydratedComponents) > 0 {
				for i, c := range tt.want.hydratedComponents {
					if i > 0 {
						metadata += ", "
					}
					metadata += c
				}
			}
			metadata += "]"
			// metadata.clientOnlyComponents
			metadata += ", clientOnlyComponents: ["
			if len(tt.want.metadata.clientOnlyComponents) > 0 {
				for i, c := range tt.want.clientOnlyComponents {
					if i > 0 {
						metadata += ", "
					}
					metadata += fmt.Sprintf("'%s'", c)
				}
			}
			metadata += "]"
			// directives
			metadata += ", hydrationDirectives: new Set(["
			if len(tt.want.hydrationDirectives) > 0 {
				for i, c := range tt.want.hydrationDirectives {
					if i > 0 {
						metadata += ", "
					}
					metadata += fmt.Sprintf("'%s'", c)
				}
			}
			metadata += "])"
			// metadata.hoisted
			metadata += ", hoisted: ["
			if len(tt.want.metadata.hoisted) > 0 {
				for i, h := range tt.want.hoisted {
					if i > 0 {
						metadata += ", "
					}
					metadata += h
				}
			}
			metadata += "] }"

			toMatch += "\n\n" + fmt.Sprintf("export const %s = %s(import.meta.url, %s);\n\n", METADATA, CREATE_METADATA, metadata)
			toMatch += test_utils.Dedent(CREATE_ASTRO_CALL) + "\n\n"
			if tt.want.skipHoist != true && len(tt.want.getStaticPaths) > 0 {
				toMatch += strings.TrimSpace(test_utils.Dedent(tt.want.getStaticPaths)) + "\n\n"
			}
			toMatch += test_utils.Dedent(PRELUDE) + "\n"
			if len(tt.want.frontmatter) > 1 {
				toMatch += test_utils.Dedent(tt.want.frontmatter[1])
			}
			toMatch += "\n"
			if len(tt.want.styles) > 0 {
				toMatch = toMatch + STYLE_PRELUDE
				for _, style := range tt.want.styles {
					toMatch += style + ",\n"
				}
				toMatch += STYLE_SUFFIX
			}
			if len(tt.want.scripts) > 0 {
				toMatch = toMatch + SCRIPT_PRELUDE
				for _, script := range tt.want.scripts {
					toMatch += script + ",\n"
				}
				toMatch += SCRIPT_SUFFIX
			}
			// code
			toMatch += test_utils.Dedent(fmt.Sprintf("%s%s", RETURN, tt.want.code))
			// HACK: add period to end of test to indicate significant preceding whitespace (otherwise stripped by dedent)
			if strings.HasSuffix(toMatch, ".") {
				toMatch = strings.TrimRight(toMatch, ".")
			}
			toMatch += SUFFIX

			// compare to expected string, show diff if mismatch
			if diff := test_utils.ANSIDiff(test_utils.Dedent(toMatch), test_utils.Dedent(output)); diff != "" {
				t.Error(fmt.Sprintf("mismatch (-want +got):\n%s", diff))
			}
		})
	}
}

func TestPrintToJSON(t *testing.T) {
	tests := []jsonTestcase{
		{
			name:   "basic",
			source: `<h1>Hello world!</h1>`,
			want:   []ASTNode{{Type: "element", Name: "h1", Children: []ASTNode{{Type: "text", Value: "Hello world!"}}}},
		},
		{
			name:   "expression",
			source: `<h1>Hello {world}</h1>`,
			want:   []ASTNode{{Type: "element", Name: "h1", Children: []ASTNode{{Type: "text", Value: "Hello "}, {Type: "expression", Children: []ASTNode{{Type: "text", Value: "world"}}}}}},
		},
		{
			name:   "Component",
			source: `<Component />`,
			want:   []ASTNode{{Type: "component", Name: "Component"}},
		},
		{
			name:   "custom-element",
			source: `<custom-element />`,
			want:   []ASTNode{{Type: "custom-element", Name: "custom-element"}},
		},
		{
			name:   "Doctype",
			source: `<!DOCTYPE html />`,
			want:   []ASTNode{{Type: "doctype", Value: "html"}},
		},
		{
			name:   "Comment",
			source: `<!--hello-->`,
			want:   []ASTNode{{Type: "comment", Value: "hello"}},
		},
		{
			name:   "Comment preserves whitespace",
			source: `<!-- hello -->`,
			want:   []ASTNode{{Type: "comment", Value: " hello "}},
		},
		{
			name:   "Fragment Shorthand",
			source: `<>Hello</>`,
			want:   []ASTNode{{Type: "fragment", Name: "", Children: []ASTNode{{Type: "text", Value: "Hello"}}}},
		},
		{
			name:   "Fragment Literal",
			source: `<Fragment>World</Fragment>`,
			want:   []ASTNode{{Type: "fragment", Name: "Fragment", Children: []ASTNode{{Type: "text", Value: "World"}}}},
		},
		{
			name: "Frontmatter",
			source: `---
const a = "hey"
---
<div>{a}</div>`,
			want: []ASTNode{{Type: "frontmatter", Value: "\nconst a = \"hey\"\n"}, {Type: "element", Name: "div", Children: []ASTNode{{Type: "expression", Children: []ASTNode{{Type: "text", Value: "a"}}}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// transform output from source
			code := test_utils.Dedent(tt.source)

			doc, err := astro.Parse(strings.NewReader(code))

			if err != nil {
				t.Error(err)
			}

			root := ASTNode{Type: "root", Children: tt.want}
			toMatch := root.String()

			result := PrintToJSON(code, doc, types.ParseOptions{Position: false})

			if diff := test_utils.ANSIDiff(test_utils.Dedent(string(toMatch)), test_utils.Dedent(string(result.Output))); diff != "" {
				t.Error(fmt.Sprintf("mismatch (-want +got):\n%s", diff))
			}
		})
	}
}
