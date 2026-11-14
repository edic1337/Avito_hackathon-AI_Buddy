// Optional SQL smoke test for hosts that cannot run native Go test binaries.
// Run from backend: node scripts/verify-sql.cjs /path/to/pglite-installation
// Executes SQL extracted from Go source against an isolated in-memory database.
const {createRequire}=require('node:module');
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
const {execFileSync}=require('node:child_process');
const localRequire=createRequire(path.join(process.argv[2],'package.json'));
const {PGlite}=localRequire('@electric-sql/pglite');
const {vector}=localRequire('@electric-sql/pglite-pgvector');
const crypto=require('node:crypto');
const hash=s=>crypto.createHash('sha256').update(s).digest('hex');
const root=process.cwd();
const source=fs.readFileSync('rag/store.go','utf8');
const statements=[...source.matchAll(/`([^`]+)`/g)].map(x=>x[1]);
const sql=prefix=>statements.find(x=>x.startsWith(prefix));
const search=fs.readFileSync('rag/search.go','utf8').match(/const searchSQL = `([\s\S]+?)`/)[1];
const v=i=>Array.from({length:1024},(_,n)=>n===i?1:0);
const literal=v=>JSON.stringify(v);
(async()=>{
 const db=await PGlite.create({extensions:{vector}});
 try {
  for(let repeat=0;repeat<2;repeat++)for(const file of ['001_rag_schema.sql','002_optional_token_count.sql'])await db.exec(fs.readFileSync('rag/migrations/'+file,'utf8'));
  const profile={tokenizer_model:'Qwen/Qwen3-Embedding-0.6B',target_tokens:300,max_tokens:400,overlap_tokens:40};
  await db.query(sql('INSERT INTO rag.collections'),['test','Qwen/Qwen3-Embedding-0.6B','',JSON.stringify(profile)]);
  const collection=(await db.query(sql('SELECT id FROM rag.collections'),['test','Qwen/Qwen3-Embedding-0.6B','',JSON.stringify(profile)])).rows[0].id;
  assert.equal((await db.query(sql('SELECT id FROM rag.collections'),['test','Qwen/Qwen3-Embedding-0.6B','wrong',JSON.stringify(profile)])).rows.length,0);
  async function save(key,role,team,texts,embeddings){
   await db.exec('BEGIN');
   try {
    const id=(await db.query(sql('INSERT INTO rag.documents'),[collection,key,'',key,texts.join('\n'),'demo://'+key,'synthetic',role,team,[],hash('content')])).rows[0].id;
    await db.query(sql('DELETE FROM rag.chunks'),[id]);
    for(let i=0;i<texts.length;i++)await db.query(sql('INSERT INTO rag.chunks'),[id,i+1,'',texts[i],key+'\n'+texts[i],null,hash(texts[i]),literal(embeddings[i])]);
    await db.exec('COMMIT');return id;
   } catch(e){await db.exec('ROLLBACK');throw e;}
  }
  const q=(role=1,team='',question='avito-cli',vec=v(0),limit=6,candidate=30)=>db.query(search,[collection,role,team,literal(vec),question,candidate,.45,limit]);
  const id=await save('general','intern','',['Общие сведения'],[v(0)]);
  assert.equal(await save('general','intern','',['Общие сведения'],[v(0)]),id);
  const mixed=v(0);mixed[0]=.8;mixed[1]=.6;
  await save('tool','intern','',['Доступ к avito-cli'],[mixed]);
  await save('secret','buddy','',['avito-cli для наставника'],[v(0)]);
  await save('other','intern','other',['avito-cli другой команды'],[v(0)]);
  await save('own','intern','mine',['avito-cli своей команды'],[v(0)]);
  await save('weak','intern','',['avito-cli'],[v(1)]);
  assert.deepEqual((await q()).rows.map(x=>x.title),['tool','general']);
  assert.equal((await q(1,'mine')).rows.length,3);
  assert.equal((await q(2)).rows.length,3);
  const manager=(await q(3,'mine')).rows;assert.equal(manager.length,4);assert.ok(!manager.some(x=>x.title==='other'));
  assert.equal((await q(1,'','unrelated',v(2))).rows.length,0);
  assert.equal((await q(1,'','avito-cli',v(0),1,1)).rows.length,1);
  await assert.rejects(save('general','manager','',['new','bad'],[v(0),[1]]));
  assert.ok((await q()).rows.some(x=>x.title==='general'&&x.text==='Общие сведения'));
  await db.query(sql('UPDATE rag.documents SET is_active'),[collection,'tool']);
  assert.equal((await q()).rows.length,1);
  const exportData=JSON.parse(execFileSync('git',['show','origin/RAG:embeddings.json'],{cwd:path.dirname(root),maxBuffer:20*1024*1024,encoding:'utf8'}));
  assert.equal(exportData.embedding_model,'Qwen/Qwen3-Embedding-0.6B');
  assert.deepEqual(exportData.chunking,profile);
  await db.exec('BEGIN');
  const d=exportData.document;
  const imported=(await db.query(sql('INSERT INTO rag.documents'),[collection,'playbook/ds-skills.md',d.source_path,d.title,d.text,d.source_url,d.origin,d.min_role,d.team??'',d.tags,hash(JSON.stringify([d.title,d.text]))])).rows[0].id;
  for(const c of exportData.chunks){
   assert.equal(c.embedding.length,1024);
   const norm=Math.hypot(...c.embedding);const emb=c.embedding.map(x=>x/norm);
   const text=d.title+(c.section.trim()?' | '+c.section:'')+'\n'+c.text;
   await db.query(sql('INSERT INTO rag.chunks'),[imported,c.index,c.section,c.text,text,c.token_count,hash(text),literal(emb)]);
  }
  await db.exec('COMMIT');
  const importedRows=(await db.query('SELECT count(*)::int AS n FROM rag.chunks WHERE doc_id=$1',[imported])).rows[0].n;
  assert.equal(importedRows,exportData.chunks.length);
  const results=await q(1,'','DS1',exportData.chunks[0].embedding);
  assert.ok(results.rows.some(x=>x.title===d.title));
  console.log(JSON.stringify({status:'PASS',checks:['migrations repeated','collection profile','idempotent upsert','hybrid RRF ranking','intern/buddy/manager ACL','team ACL','cosine threshold','candidate limit','transaction rollback','deactivation','real RAG export inserted and retrieved'],importedRows}));
 } finally {await db.close();}
})().catch(e=>{console.error(e);process.exitCode=1});
