import { test } from 'uvu';
import * as assert from 'uvu/assert';
import { parse } from '@astrojs/compiler';
import fs from 'fs';

const FIXTURE = `
---
const sections = [{ label: 1, items: [] }];
---
<div>
  {sections.map(({ label, items }) => (
    <Fragment>
      {label ? "truthy" : "falsy"}
    </Fragment>
  ))}
</div>
`;

let result;
test.before(async () => {
  result = await parse(FIXTURE);
});

test('nested expression in fragment shorthand', () => {
  fs.writeFileSync('./out.json', JSON.stringify(result, null, 2));
});

test.run();
