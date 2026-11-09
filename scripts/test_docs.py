from pathlib import Path
import tempfile
import unittest

import docs


class PublicationTests(unittest.TestCase):
    def test_rewrite_preserves_code_and_resolves_published_and_source_links(self):
        source = '[Guide](running.md#configuration)\n\n`[Guide](running.md)`\n\n```md\n[Guide](running.md)\n```\n\n[Code](../cmd/discord/main.go)\n'
        result = docs.rewrite('docs/README.md', source, 'site', 'abc123', '/melodix/')
        self.assertIn('[Guide](</melodix/docs/running.html#configuration>)', result)
        self.assertIn('`[Guide](running.md)`', result)
        self.assertIn('```md\n[Guide](running.md)\n```', result)
        self.assertIn('https://github.com/keshon/melodix/blob/abc123/cmd/discord/main.go', result)
        raw = docs.rewrite('docs/README.md', '[Guide](running.md)', 'raw', 'abc123')
        self.assertIn('https://melodix.keshon.ru/docs/running.md', raw)
        with self.assertRaisesRegex(ValueError, 'escapes'):
            docs.local_target('README.md', '../outside')

    def test_manifest_has_only_maintained_human_articles(self):
        docs.validate_manifest()
        for page in docs.PAGES:
            self.assertNotIn('/internal/', page['source'])
            self.assertNotIn('skills', page['source'])
            # Parsing all current articles also rejects missing repository paths.
            docs.rewrite(page['source'], docs.read(docs.ROOT / page['source']), 'site', 'abc123')

    def test_bundle_keeps_local_guides_and_pins_omitted_sources(self):
        (docs.ROOT / 'dist').mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=docs.ROOT / 'dist') as temporary:
            destination = Path(temporary)
            binary = destination / 'melodix-discord.exe'
            binary.write_bytes(b'synthetic binary selection fixture')
            docs.archive(destination)
            self.assertEqual(binary.read_bytes(), b'synthetic binary selection fixture')
            self.assertTrue((destination / '.env').is_file())
            self.assertFalse((destination / 'docs/internal').exists())
            self.assertFalse((destination / '.agents').exists())
            self.assertFalse((destination / 'README.md.tmpl').exists())
            self.assertEqual((destination / '.env').read_bytes(), (docs.ROOT / '.env.example').read_bytes())
            self.assertIn('(<assets/readme-banner.webp>)', docs.read(destination / 'README.md'))
            architecture = docs.read(destination / 'docs/architecture.md')
            self.assertIn(f'/blob/{docs.revision()}/docs/internal/implementation-notes.md', architecture)
            for source in ['README.md'] + list(docs.BY_SOURCE):
                text = docs.read(destination / source)
                for match in docs.TOKENS.finditer(text):
                    if match.group('dest') is None:
                        continue
                    href = match.group('dest').strip('<>')
                    url = docs.urlsplit(href)
                    if url.scheme or url.netloc:
                        continue
                    target = (destination / source).parent / docs.unquote(url.path)
                    if not url.path:
                        target = destination / source
                    self.assertTrue(target.exists(), f'{source}: {href}')

    def test_bundle_refuses_existing_runtime_or_private_files(self):
        (docs.ROOT / 'dist').mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=docs.ROOT / 'dist') as temporary:
            destination = Path(temporary)
            (destination / 'private.txt').write_text('synthetic private data', encoding='utf-8')
            with self.assertRaisesRegex(ValueError, 'Unexpected bundle files'):
                docs.archive(destination)

    def test_output_verifier_rejects_unlisted_content(self):
        if not (docs.SITE / 'index.html').exists():
            self.skipTest('Run after a site build for the negative publication check')
        unexpected = docs.SITE / 'unlisted.html'
        self.assertFalse(unexpected.exists())
        try:
            unexpected.write_text('<h1>Unlisted</h1>', encoding='utf-8')
            with self.assertRaisesRegex(ValueError, 'HTML'):
                docs.verify('/')
        finally:
            unexpected.unlink()


if __name__ == '__main__':
    unittest.main()
