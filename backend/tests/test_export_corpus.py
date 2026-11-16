import json
import math
from pathlib import Path
import tempfile
import unittest

from backend.rag.export_corpus import CorpusChunker, assemble, atomic_json, load_source, normalize_html
from backend.rag.models import Document


class Tokenizer:
    def encode(self, text, **kwargs):
        return list(text)


class CorpusExportTests(unittest.TestCase):
    def test_public_import_preserves_url_and_removes_frontmatter_from_body(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'123.md'
            p.write_text('---\ntitle: Demo\nsource_url: https://habr.com/ru/companies/avito/articles/123/\ncontent_type: full_article\n---\n# Intro\n\nBody',encoding='utf-8')
            doc,meta=load_source(p,True)
            self.assertEqual((doc.origin,doc.min_role,doc.team),('public','intern',None))
            self.assertEqual(doc.text,'# Intro\n\nBody')
            self.assertEqual(meta['content_type'],'full_article')
            with self.assertRaises(ValueError):load_source(p,False)
            original=p.read_text(encoding='utf-8')
            p.write_text(original.replace('/avito/','/oleg-bunin/'),encoding='utf-8')
            self.assertIn('/oleg-bunin/',load_source(p,True)[0].source_url)
            p.write_text(original.replace('habr.com','example.com'),encoding='utf-8')
            with self.assertRaises(ValueError):load_source(p,True)
            p.write_text(original,encoding='utf-8')
            p.write_text(p.read_text().replace('full_article','summary'),encoding='utf-8')
            with self.assertRaises(ValueError):load_source(p,True)

    def test_public_import_does_not_weaken_existing_acl(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'123.md'
            p.write_text('---\ntitle: Demo\nsource_url: https://habr.com/ru/companies/avito/articles/123/\ncontent_type: full_article\nmin_role: manager\n---\nBody',encoding='utf-8')
            with self.assertRaisesRegex(ValueError,'conflicts'):load_source(p,True)

    def test_html_table_spans_preserve_row_context_and_fenced_code_is_untouched(self):
        table='<table><tr><th>Team</th><th>Duty</th></tr><tr><td rowspan="2">QA</td><td>Test</td></tr><tr><td>Review</td></tr></table>'
        result=normalize_html(table+'\n\n```html\n'+table+'\n```\n')
        self.assertIn('| QA | Test |',result)
        self.assertIn('| QA | Review |',result)
        self.assertIn('```html\n'+table+'\n```',result)

    def test_oversized_table_cell_keeps_values_and_budget(self):
        chunker=CorpusChunker(Tokenizer(),target_tokens=100,max_tokens=150,overlap_tokens=0)
        text='| Key | Value |\n| --- | --- |\n| QA | '+' '.join('marker'+str(i) for i in range(60))+' |'
        chunks=chunker.chunk(Document('Demo',text,'demo://test','synthetic','intern'))
        self.assertGreater(len(chunks),1)
        self.assertTrue(all(chunker.count_tokens(c.embedding_text('Demo'))<=150 for c in chunks))
        combined='\n'.join(c.text for c in chunks)
        for i in range(60):self.assertIn('marker'+str(i),combined)
        self.assertTrue(all('column:' in c.text for c in chunks))

    def test_assembled_json_has_full_vectors_and_explicit_incomplete_state(self):
        with tempfile.TemporaryDirectory() as d:
            d=Path(d);p=d/'doc.json'
            atomic_json(p,{'document':{'source_key':'test.md','chunks':[{'embedding':[1.0]+[0.0]*1023}]}})
            output=d/'corpus.json'
            self.assertEqual(assemble(output,[p],{'embeddings_included':True},[],1),1)
            result=json.loads(output.read_text(encoding='utf-8'))
            self.assertTrue(result['summary']['complete'])
            self.assertEqual(len(result['documents'][0]['chunks'][0]['embedding']),1024)
            assemble(output,[p],{},[{'error':'missing file'}],2)
            self.assertFalse(json.loads(output.read_text())['summary']['complete'])


if __name__=='__main__':unittest.main()
