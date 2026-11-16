import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from backend.rag import config as config_module
from backend.rag.config import RagConfig


class EnvFileTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.root = Path(self.folder.name)
        self.path = self.root / '.env'

    def test_project_env_works_independently_of_current_directory(self):
        self.path.write_text('EMBEDDING_DEVICE=cuda\nEMBEDDING_BATCH_SIZE=3\n', encoding='utf-8')
        with patch.object(config_module, 'PROJECT_ROOT', self.root), patch.dict(os.environ, {}, clear=True):
            config = RagConfig.from_env()
        self.assertEqual((config.embedding_device, config.embedding_batch_size), ('cuda', 3))

    def test_process_values_override_file_without_global_mutation(self):
        self.path.write_text('EMBEDDING_DEVICE=cuda\nTOP_K=9\n', encoding='utf-8')
        with patch.dict(os.environ, {'EMBEDDING_DEVICE': 'cpu'}, clear=True):
            config = RagConfig.from_env(self.path)
            self.assertEqual(dict(os.environ), {'EMBEDDING_DEVICE': 'cpu'})
        self.assertEqual(config.embedding_device, 'cpu')
        self.assertEqual(config.top_k, 9)

    def test_bom_quotes_unicode_and_dollar_sign_in_credentials(self):
        self.path.write_text('LLM_API_KEY="secret-${NAME}-token"\nLLM_MODEL="модель"\nLLM_MOCK=true\n', encoding='utf-8-sig')
        with patch.dict(os.environ, {}, clear=True):
            config = RagConfig.from_env(self.path)
        self.assertEqual(config.llm_api_key, 'secret-${NAME}-token')
        self.assertEqual(config.llm_model, 'модель')
        self.assertTrue(config.llm_mock)

    def test_none_disables_dotenv_and_missing_file_uses_defaults(self):
        self.path.write_text('TOP_K=17\n', encoding='utf-8')
        with patch.object(config_module, 'PROJECT_ROOT', self.root), patch.dict(os.environ, {}, clear=True):
            self.assertEqual(RagConfig.from_env(None), RagConfig())
            self.assertEqual(RagConfig.from_env(self.root/'missing.env'), RagConfig())

    def test_tokenizer_follows_embedding_model_unless_explicit(self):
        self.path.write_text('EMBEDDING_MODEL=local/model\n', encoding='utf-8')
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(RagConfig.from_env(self.path).tokenizer_model, 'local/model')
            self.path.write_text('EMBEDDING_MODEL=local/model\nTOKENIZER_MODEL=local/tokenizer\n', encoding='utf-8')
            self.assertEqual(RagConfig.from_env(self.path).tokenizer_model, 'local/tokenizer')

    def test_invalid_limits_are_rejected_before_model_loading(self):
        for value in ['EMBEDDING_BATCH_SIZE=0', 'CHUNK_OVERLAP_TOKENS=400', 'TOP_K=0', 'MIN_SIMILARITY=2', 'EMBEDDING_DEVICE=unknown']:
            with self.subTest(value=value), patch.dict(os.environ, {}, clear=True):
                self.path.write_text(value+'\n', encoding='utf-8')
                with self.assertRaises(ValueError):
                    RagConfig.from_env(self.path)


if __name__ == '__main__':
    unittest.main()
