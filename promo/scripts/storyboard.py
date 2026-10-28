from pathlib import Path
import subprocess
from PIL import Image,ImageDraw
root=Path(__file__).resolve().parent.parent
out=root/'out'
times=[2,7,13,19,26,32,39,44]
board=Image.new('RGB',(1280,4*388),'#e8eef7')
for i,t in enumerate(times):
 target=out/f'scene-{i+1:02d}.jpg'
 subprocess.run(['ffmpeg','-v','error','-y','-ss',str(t),'-i',str(out/'Silieco-产品宣传.mp4'),'-frames:v','1','-vf','scale=640:360',str(target)],check=True)
 board.paste(Image.open(target),((i%2)*640,(i//2)*388))
 ImageDraw.Draw(board).text(((i%2)*640+16,(i//2)*388+366),f'{i+1:02d} / 00:{t:02d}',fill='#65738a')
board.save(out/'故事板.jpg',quality=94)
