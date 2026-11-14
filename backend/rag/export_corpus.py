"""Batch Markdown -> chunks -> Qwen embeddings -> database-ready JSON.

Run from the project root: python -m backend.rag.export_corpus --help
"""
import argparse
import asyncio
import hashlib
import json
import math
import os
import tempfile
import time
from dataclasses import asdict
from pathlib import Path
from urllib.parse import urlparse

import yaml

from .chunking import MarkdownChunker
from .config import RagConfig
from .documents import parse_document
from .embeddings import QwenEmbedder
from .models import Document, TextChunk


def digest(value):
    return hashlib.sha256(json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def atomic_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    name = None
    try:
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, delete=False) as f:
            name = Path(f.name)
            json.dump(value, f, ensure_ascii=False, allow_nan=False, separators=(",", ":"))
            f.write("\n")
        os.replace(name, path)
    finally:
        if name is not None:
            name.unlink(missing_ok=True)


def load_source(path, public_habr=False):
    """Public access is explicit and restricted to the downloaded Habr corpus."""
    raw = path.read_text(encoding="utf-8-sig")
    if not public_habr:
        return parse_document(raw, source_path=str(path)), {}
    lines = raw.splitlines(keepends=True)
    if not lines or lines[0].strip() != "---":
        raise ValueError("Public Habr input requires YAML metadata")
    end = next((i for i in range(1, len(lines)) if lines[i].strip() == "---"), None)
    if end is None:
        raise ValueError("Unclosed YAML metadata")
    header = "".join(lines[1:end])
    node = yaml.compose(header, Loader=yaml.SafeLoader)
    if not isinstance(node, yaml.MappingNode) or any(not isinstance(k, yaml.ScalarNode) for k, _ in node.value):
        raise ValueError("Metadata must be a mapping with scalar keys")
    keys = [key.value for key, _ in node.value]
    if len(keys) != len(set(keys)) or "<<" in keys:
        raise ValueError("Duplicate or merged metadata keys")
    meta = yaml.safe_load(header)
    url = meta.get("source_url", "")
    parsed = urlparse(url)
    import re
    if parsed.scheme != "https" or parsed.netloc != "habr.com" or not re.fullmatch(r"/ru/(?:companies/[a-zA-Z0-9_-]+/(?:articles|posts)|specials)/\d+/", parsed.path):
        raise ValueError("--public-habr only accepts public Habr company article or special URLs")
    if meta.get("content_type") != "full_article":
        raise ValueError("Expected full_article; catalogs and summaries cannot be embedded")
    # Existing ACL metadata is never silently weakened by a conversion flag.
    if meta.get("origin", "public") != "public" or meta.get("min_role", "intern") != "intern" or meta.get("team") is not None:
        raise ValueError("--public-habr conflicts with source access metadata")
    normalized = dict(meta, origin="public", min_role="intern", team=None)
    text = "---\n" + yaml.safe_dump(normalized, allow_unicode=True) + "---\n" + "".join(lines[end+1:])
    doc = parse_document(text, source_path=str(path))
    return doc, meta


def normalize_html(text):
    """Remove empty anchors; preserve details' text and convert HTML tables."""
    import re
    from bs4 import BeautifulSoup
    def replace_table(match):
        table = BeautifulSoup(match.group(), "html.parser").find("table")
        grid = []
        spans = {}
        headers = 0
        for row in table.find_all("tr"):
            if row.find_parent("table") is not table:
                continue
            values = []; col = 0
            cells = row.find_all(["td", "th"], recursive=False)
            if not cells:
                continue
            if all(c.name == "th" for c in cells) and len(grid) == headers:
                headers += 1
            def prior():
                nonlocal col
                while col in spans:
                    value, count = spans[col]; values.append(value)
                    if count <= 1: del spans[col]
                    else: spans[col] = (value, count-1)
                    col += 1
            for cell in cells:
                prior()
                value = cell.get_text(" ", strip=True).replace("|", "\\|").replace("\n", " ")
                for _ in range(int(cell.get("colspan", 1))):
                    values.append(value)
                    rows = int(cell.get("rowspan", 1))
                    if rows > 1: spans[col] = (value, rows-1)
                    col += 1
            prior();grid.append(values)
        if not grid:
            return "\n\n" + table.get_text(" ", strip=True) + "\n\n"
        width = max(map(len, grid));grid = [r + [""]*(width-len(r)) for r in grid]
        if headers:
            names = [" / ".join(dict.fromkeys(r[c] for r in grid[:headers] if r[c])) or f"Column {c+1}" for c in range(width)]
            data = grid[headers:]
        else:
            names = [f"Column {c+1}" for c in range(width)];data = grid
        line = lambda r: "| " + " | ".join(r) + " |"
        return "\n\n" + "\n".join([line(names), line(["---"]*width)] + [line(r) for r in data]) + "\n\n"
    # Do not interpret or rewrite fenced code as HTML.
    lines = text.splitlines(keepends=True);out=[];plain=[];fence=None
    def flush():
        value = "".join(plain)
        value = re.sub(r"<table\b[^>]*>.*?</table\s*>", replace_table, value, flags=re.I|re.S)
        value = re.sub(r'<a\b[^>]*(?:id|name)=[^>]*>\s*</a>', '', value, flags=re.I)
        value = re.sub(r'</?(?:details|summary)\b[^>]*>', '', value, flags=re.I)
        out.append(value);plain.clear()
    for line in lines:
        m = re.match(r"^\s*(`{3,}|~{3,})",line)
        if fence is None and m:
            flush();fence=m.group(1);out.append(line)
        elif fence is not None:
            out.append(line)
            if m and m.group(1)[0]==fence[0] and len(m.group(1))>=len(fence):fence=None
        else:plain.append(line)
    flush();return "".join(out)


class CorpusChunker(MarkdownChunker):
    """Keep the existing chunker, with explicit handling of oversized table cells."""
    def _split_block(self, block, fits):
        try:
            return super()._split_block(block, fits)
        except ValueError:
            if block.kind != "table":
                raise
        from .chunking import _Block
        rows=[];row=[]
        for token in self._parser.parse(block.text):
            if token.type == "tr_open":row=[]
            elif token.type == "inline":row.append(token.content)
            elif token.type == "tr_close":rows.append(row)
        if len(rows)<2:
            raise ValueError("Cannot split table without header and data rows")
        headers=rows[0];pieces=[]
        for number, row in enumerate(rows[1:],1):
            for index, value in enumerate(row):
                if not value.strip():continue
                header=headers[index] if index<len(headers) else str(index+1)
                prefix=f"Table row {number}; key: {row[0]}; column: {header}\n"
                if not fits(prefix+'x'):
                    # Avoid replicating an oversized row key (the key itself is still exported as its own cell).
                    prefix=f"Table row {number}; column {index+1}\n"
                wrap=lambda s,prefix=prefix:prefix+s
                pieces.extend(_Block(part,"table") for part in self._split_text(value,lambda s:fits(wrap(s)),wrap))
        return pieces


def collection_config(config, revision, chunks_only):
    return {
        "name": "avito-habr", "embedding_model": config.embedding_model,
        "embedding_revision": revision, "embedding_dimension": QwenEmbedder.dimension,
        "embedding_text_version": 1, "query_prompt_name": "query",
        "chunking_config": {"tokenizer_model":config.tokenizer_model, "target_tokens":config.chunk_target_tokens,
                            "max_tokens":config.chunk_max_tokens, "overlap_tokens":config.chunk_overlap_tokens,
                            "html_normalization_version":1},
        "embeddings_included":not chunks_only,
    }


def assemble(output, paths, collection, errors, expected):
    """Stream checkpoints into one JSON instead of retaining all vectors in RAM."""
    output=Path(output);output.parent.mkdir(parents=True,exist_ok=True);name=None;count=0
    try:
        with tempfile.NamedTemporaryFile('w',encoding='utf-8',dir=output.parent,delete=False) as f:
            name=Path(f.name)
            f.write('{"schema_version":2,"collection":');json.dump(collection,f,ensure_ascii=False,allow_nan=False)
            f.write(',"documents":[')
            for i,path in enumerate(paths):
                data=json.loads(path.read_text(encoding='utf-8'))['document'];count+=len(data['chunks'])
                if i:f.write(',')
                json.dump(data,f,ensure_ascii=False,allow_nan=False,separators=(',',':'))
            f.write('],"summary":');json.dump(dict(expected_documents=expected,documents=len(paths),chunks=count,complete=not errors and len(paths)==expected),f)
            f.write(',"errors":');json.dump(errors,f,ensure_ascii=False);f.write('}\n')
        os.replace(name,output)
    finally:
        if name is not None:name.unlink(missing_ok=True)
    return count


async def run(args):
    from transformers import AutoTokenizer
    config=RagConfig.from_env()
    if args.device: config=__import__('dataclasses').replace(config,embedding_device=args.device)
    if args.threads:
        import torch
        torch.set_num_threads(args.threads)
    root=args.input.resolve();output=args.output.resolve()
    if not root.is_dir():raise ValueError('--input must be a directory')
    files=sorted(p for p in root.rglob('*') if p.suffix.lower() in ('.md','.markdown') and p.name.lower() not in ('index.md','readme.md','chunking.md'))
    if args.limit:files=files[:args.limit]
    if not files:raise ValueError('No input Markdown documents')
    print(f'Loading tokenizer: {config.tokenizer_model}',flush=True)
    tokenizer=AutoTokenizer.from_pretrained(config.tokenizer_model,local_files_only=args.offline)
    revision=getattr(tokenizer,'init_kwargs',{}).get('_commit_hash')
    chunker=CorpusChunker(tokenizer,target_tokens=config.chunk_target_tokens,max_tokens=config.chunk_max_tokens,overlap_tokens=config.chunk_overlap_tokens)
    collection=collection_config(config,revision,args.chunks_only)
    checkpoint_dir=output.parent/(output.stem+'.checkpoints');checkpoint_dir.mkdir(parents=True,exist_ok=True)
    embedder=QwenEmbedder(config);done=[];errors=[];started=time.monotonic();total_chunks=0
    for index,path in enumerate(files,1):
        key=path.relative_to(root).as_posix()
        try:
            document,meta=load_source(path,args.public_habr)
            content_hash=digest([document.title,document.text])
            fingerprint=digest(dict(collection=collection,document=asdict(document),source_key=key,normalization=1))
            target=checkpoint_dir/(digest(key)+'.json')
            if args.resume and target.exists():
                cached=json.loads(target.read_text(encoding='utf-8'))
                if cached.get('fingerprint')==fingerprint:
                    done.append(target);total_chunks+=len(cached['document']['chunks'])
                    print(f'[{index}/{len(files)}] cached {key}',flush=True);continue
            prepared=__import__('dataclasses').replace(document,text=normalize_html(document.text))
            chunks=chunker.chunk(prepared)
            if not chunks:raise ValueError('Document produced no chunks')
            chunk_rows=[]
            for offset in range(0,len(chunks),config.embedding_batch_size):
                batch=chunks[offset:offset+config.embedding_batch_size]
                texts=[c.embedding_text(document.title) for c in batch]
                vectors=await embedder.embed_documents(texts) if not args.chunks_only else [None]*len(batch)
                for position,(chunk,embedding_text,vector) in enumerate(zip(batch,texts,vectors,strict=True),offset+1):
                    item=dict(position=position,section=chunk.section,text=chunk.text,embedding_text=embedding_text,
                              token_count=chunker.count_tokens(embedding_text,special_tokens=True),content_hash=hashlib.sha256(embedding_text.encode()).hexdigest())
                    if vector is not None:
                        if len(vector)!=1024 or not all(math.isfinite(v) for v in vector) or abs(math.hypot(*vector)-1)>1e-6:
                            raise ValueError('Invalid embedding')
                        item['embedding']=vector
                    chunk_rows.append(item)
            data=dict(source_key=key,source_path=str(path),title=document.title,content=document.text,source_url=document.source_url,
                      origin=document.origin,min_role=document.min_role,team=document.team,tags=list(document.tags),content_hash=content_hash,
                      metadata=meta,chunks=chunk_rows)
            atomic_json(target,dict(fingerprint=fingerprint,document=data));done.append(target);total_chunks+=len(chunk_rows)
            print(f'[{index}/{len(files)}] {key}: {len(chunks)} chunks; total={total_chunks}; elapsed={time.monotonic()-started:.1f}s',flush=True)
        except Exception as exc:
            errors.append(dict(source_key=key,error=str(exc)));print(f'ERROR {key}: {exc}',flush=True)
            if args.fail_fast:break
    count=assemble(output,done,collection,errors,len(files))
    print(json.dumps(dict(output=str(output),documents=len(done),chunks=count,errors=len(errors),embeddings=not args.chunks_only,elapsed_seconds=round(time.monotonic()-started,2)),ensure_ascii=False),flush=True)
    return 1 if errors else 0


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--public-habr',action='store_true',help='Explicitly import full public Avito Habr articles with intern access')
    parser.add_argument('--chunks-only',action='store_true',help='Skip embeddings; output is NOT ready for vector DB import')
    parser.add_argument('--resume',action='store_true',help='Reuse checkpoints with matching input and settings')
    parser.add_argument('--offline',action='store_true',help='Use cached tokenizer; set HF_HUB_OFFLINE=1 for cached model too')
    parser.add_argument('--device',choices=['cpu','cuda'])
    parser.add_argument('--threads',type=int,default=4)
    parser.add_argument('--limit',type=int)
    parser.add_argument('--fail-fast',action='store_true')
    args=parser.parse_args()
    if args.threads<1 or (args.limit is not None and args.limit<1):parser.error('threads and limit must be positive')
    if args.output.suffix.lower()!='.json' or args.output.is_dir():parser.error('--output must be a .json file')
    raise SystemExit(asyncio.run(run(args)))


if __name__=='__main__':main()
