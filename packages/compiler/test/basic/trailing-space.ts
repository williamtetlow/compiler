import { test } from 'uvu';
import * as assert from 'uvu/assert';
import { transform } from '@astrojs/compiler';

const FIXTURE = `---
import { Markdown } from 'astro/components';
import Layout from '../layouts/content.astro';
---

<style>
  #root {
    color: green;
  }
</style>

<Layout>
  <div id="root">
    <Markdown>
      ## Interesting Topic
    </Markdown>
  </div>
</Layout>`; // NOTE: the lack of trailing space is important to this test!

let result;
test.before(async () => {
  result = await transform(FIXTURE);
});

test('trailing space', () => {
  assert.ok(result.code, 'Expected to compiler');
  assert.not.match(result.code, 'html', 'Expected output to not contain <html>');
});

test.run();
