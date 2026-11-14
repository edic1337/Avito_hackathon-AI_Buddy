import tempfile
import unittest
from pathlib import Path

from backend.rag.documents import parse_document
from backend.rag.export_corpus import load_source


SOURCE = '''---
title: Internship checklist
source_url: attachment:///internship-checklist.csv#record=3
origin: provided
min_role: intern
source_record: 3
period: First week
---
# Task

Original task body.
'''


class ProvidedSourceTests(unittest.TestCase):
    def test_provided_source_keeps_real_provenance_and_record_metadata(self):
        with tempfile.TemporaryDirectory() as folder:
            path=Path(folder)/'item.md'
            path.write_text(SOURCE,encoding='utf-8')
            document,metadata=load_source(path)
        self.assertEqual(document.origin,'provided')
        self.assertEqual(document.min_role,'intern')
        self.assertEqual(metadata['source_record'],3)
        self.assertEqual(metadata['period'],'First week')
        self.assertIn('Original task body.',document.text)
        self.assertNotIn('source_record:',document.text)

    def test_provided_source_still_requires_explicit_access_role(self):
        with self.assertRaisesRegex(ValueError,'min_role'):
            parse_document(SOURCE.replace('min_role: intern\n',''))
        with self.assertRaises(ValueError):
            parse_document(SOURCE.replace('origin: provided','origin: unknown'))


if __name__=='__main__':unittest.main()
