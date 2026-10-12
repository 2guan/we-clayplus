#!/usr/bin/env python3
"""Scan the Git index; never print or retain candidate secret values."""
from pathlib import Path
import hashlib,json,os,shutil,subprocess,sys,tempfile
root=Path(__file__).resolve().parents[1]
raw=subprocess.check_output(['git','ls-files','-s','-z'],cwd=root)
entries=[]
for record in raw.split(b'\0'):
 if not record:continue
 meta,name=record.split(b'\t',1);mode,blob,stage=meta.decode().split();name=os.fsdecode(name)
 if stage!='0' or mode not in ('100644','100755'):raise SystemExit('Unmerged or non-regular staged file: '+name)
 p=Path(name)
 if p.is_absolute() or '..' in p.parts:raise SystemExit('Invalid staged path')
 blocked=any(x.lower() in ('runtime','data','logs','secrets','credentials','node_modules','.git','bin','dist') or x.startswith('data-restored') for x in p.parts)
 blocked=blocked or p.name.startswith('.env') or p.suffix.lower() in ('.log','.pem','.key','.p12','.pfx','.db','.sqlite','.session')
 if blocked:raise SystemExit('Forbidden staged path: '+name)
 entries.append((name,blob))
if not entries:raise SystemExit('No staged files to scan')
baseline_path=root/'security/reviewed-findings.json'
baseline=json.loads(baseline_path.read_text()) if baseline_path.exists() else []
allowed={(x['path'],x['rule'],x['digest']) for x in baseline}
scanner=os.environ.get('GITLEAKS') or shutil.which('gitleaks')
if not scanner:raise SystemExit('Install Gitleaks before pushing')
with tempfile.TemporaryDirectory(prefix='we-clay-secret-check-') as tmp:
 snapshot=Path(tmp)/'source';snapshot.mkdir();report=Path(tmp)/'findings.json'
 for name,blob in entries:
  dest=snapshot/name;dest.parent.mkdir(parents=True,exist_ok=True)
  dest.write_bytes(subprocess.check_output(['git','cat-file','blob',blob],cwd=root))
 result=subprocess.run([scanner,'dir',str(snapshot),'--report-format=json','--report-path='+str(report),'--exit-code=1','--no-banner'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 if result.returncode not in (0,1) or not report.exists():raise SystemExit('Secret scanner failed')
 findings=json.loads(report.read_text());unknown=[]
 for item in findings:
  p=Path(item['File']);name=p.relative_to(snapshot).as_posix() if p.is_absolute() else p.as_posix()
  digest=hashlib.sha256(item['Secret'].encode()).hexdigest()
  if (name,item['RuleID'],digest) not in allowed:unknown.append((name,item['StartLine'],item['RuleID']))
 if unknown:
  for name,line,rule in unknown:print(f'Unreviewed finding: {name}:{line} ({rule})')
  raise SystemExit(1)
 print(f'Secret check passed: {len(entries)} staged files; {len(findings)} reviewed static/test findings; no unreviewed findings.')
