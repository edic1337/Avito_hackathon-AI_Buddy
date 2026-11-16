import assert from 'node:assert/strict'
import { readFile, writeFile, unlink } from 'node:fs/promises'
import ts from 'typescript'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
const generated = new URL('../.message-test.mjs', import.meta.url)
try {
  const source = await readFile(new URL('../src/MessageText.tsx', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: {
    jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022,
  } })
  await writeFile(generated, compiled.outputText)
  const { MessageText } = await import(generated.href)
  const html = renderToStaticMarkup(createElement(MessageText, {
    text: '1. **Доступы** — попроси наставника [1].\n2. `код`\n\n<script>alert(1)</script>\n[опасно](javascript:alert(1))',
  }))
  assert.match(html, /<ol/)
  assert.match(html, /<strong>Доступы<\/strong>/)
  assert.match(html, /<code>код<\/code>/)
  assert.ok(!html.includes('<script>'))
  assert.ok(!html.includes('href="javascript:'))
  assert.match(html, /\[1\]/)
  console.log('PASS: formatting, citation preservation, HTML and unsafe link escaping')
} finally { await unlink(generated).catch(() => {}) }
