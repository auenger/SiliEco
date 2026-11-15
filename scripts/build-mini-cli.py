import pathlib,subprocess,os,tarfile,zipfile,hashlib,concurrent.futures
import argparse,datetime,re
parser=argparse.ArgumentParser(description="Build self-hosted CLI downloads with explicit release metadata")
parser.add_argument("--version",required=True)
parser.add_argument("--app-url",required=True)
args=parser.parse_args()
if not re.fullmatch(r"v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?",args.version):
 parser.error("--version must be an explicit semantic version")
root=pathlib.Path(__file__).resolve().parent.parent
out=root/'dist/mini-cli-release';out.mkdir(parents=True,exist_ok=True)
commit=subprocess.check_output(['git','rev-parse','--short','HEAD'],cwd=root,text=True).strip()
date=datetime.datetime.now(datetime.timezone.utc).isoformat()
flags=f"-s -w -X main.version={args.version} -X main.commit={commit} -X main.date={date}"
def build(target):
 system,arch=target
 folder=out/(system+'-'+arch);folder.mkdir(exist_ok=True)
 exe=folder/('silieco.exe' if system=='windows' else 'silieco')
 subprocess.run(['go','build','-ldflags',flags,'-o',str(exe),'./cmd/silieco'],cwd=root/'core',env=dict(os.environ,CGO_ENABLED='0',GOOS=system,GOARCH=arch),check=True)
 archive=out/('silieco-'+system+'-'+arch+('.zip' if system=='windows' else '.tar.gz'))
 if system=='windows':
  with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED) as z:z.write(exe,exe.name)
 else:
  with tarfile.open(archive,'w:gz') as t:t.add(exe,arcname=exe.name)
 return hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
 rows=list(pool.map(build,[(s,a) for s in ['darwin','linux','windows'] for a in ['arm64','amd64']]))
(out/'checksums.txt').write_text('\n'.join(rows)+'\n')
base=args.app_url.rstrip('/')+'/cli'
sh=(root/'apps/web/public/cli/install.sh').read_text().replace('BASE_URL="${SILIECO_DOWNLOAD_BASE_URL:-}"','BASE_URL="${SILIECO_DOWNLOAD_BASE_URL:-'+base+'}"')
(out/'install.sh').write_text(sh)
ps=(root/'apps/web/public/cli/install.ps1').read_text().replace('else { "" }','else { "'+base+'" }',1)
(out/'install.ps1').write_text(ps)
print('Built 6 CLI archives and mini-local installers')
