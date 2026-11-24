import assert from "node:assert/strict";
import { Readable } from "node:stream";
import { gzipSync } from "node:zlib";
import { fetchPage, publicAddress, webFetchTool } from "../server/tools/web-fetch.ts";

for (const address of ["127.0.0.1", "10.2.3.4", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "2001:db8::1", "2002:0808:0808::1"]) assert.equal(publicAddress(address), false, address);
for (const address of ["8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"]) assert.equal(publicAddress(address), true, address);
for (const url of ["http://127.0.0.1/", "http://[::1]/", "http://localhost/", "http://user:pass@example.com/"]) await assert.rejects(fetchPage(url, AbortSignal.timeout(1000)));

function response(status, headers, content) {
  const stream = Readable.from([content]);
  stream.statusCode = status;
  stream.headers = headers;
  return stream;
}
const signal = AbortSignal.timeout(5000);
let calls = 0;
const page = await fetchPage("https://example.com/start#fragment", signal, async url => {
  calls++;
  if (calls === 1) return response(302, { location: "/article" }, "");
  assert.equal(url.href, "https://example.com/article");
  return response(200, { "content-type": "text/html; charset=utf-8", "content-encoding": "gzip" }, gzipSync(Buffer.from('<html><head><title>Docs</title></head><body><main><h1>Guide</h1><p>Read <a href="/next">next</a>.</p><pre><code>hello()</code></pre></main></body></html>')));
});
assert.equal(page.requested, "https://example.com/start");
assert.equal(page.finalUrl, "https://example.com/article");
assert.match(page.content, /https:\/\/example.com\/next/);
assert.match(page.content, /hello\(\)/);
const markdown = await fetchPage("https://example.com", signal, async () => response(200, { "content-type": "text/markdown" }, "# Hello\n\n```\ncode\n```"));
assert.equal(markdown.content, "# Hello\n\n```\ncode\n```");
await assert.rejects(fetchPage("https://example.com", signal, async () => response(200, { "content-type": "application/pdf" }, "pdf")), /unsupported_media/);
await assert.rejects(fetchPage("https://example.com", signal, async () => response(200, { "content-type": "text/plain" }, Buffer.alloc(2 * 1024 * 1024 + 1))), /response_too_large/);
await assert.rejects(fetchPage("https://example.com", signal, async () => response(302, { location: "/again" }, "")), /redirect_limit/);
await assert.rejects(fetchPage("https://example.com", signal, async () => response(200, { "content-type": "text/plain" }, " ")), /empty_content/);
const canceled = new AbortController();
const slow = new Readable({ read() {} });
slow.statusCode = 200;
slow.headers = { "content-type": "text/plain" };
const pending = fetchPage("https://example.com", canceled.signal, async () => slow);
setTimeout(() => canceled.abort(), 10);
await assert.rejects(pending);
const disabled = await webFetchTool(() => false).execute({ url: "https://example.com" }, {}, {});
assert.equal(disabled.isError, true);
assert.match(disabled.output[0].text, /disabled/);
console.log("web fetch tests passed");
