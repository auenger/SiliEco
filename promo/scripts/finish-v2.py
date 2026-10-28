import json,subprocess,zipfile
from pathlib import Path
from PIL import Image,ImageDraw
root=Path(__file__).resolve().parent.parent
out=root/'out/v2'
video=out/'Silieco-人机协作宣传片.mp4'
subprocess.run(['ffmpeg','-v','error','-y','-i',str(video),'-map','0:v:0','-c:v','copy','-an',str(out/'Silieco-人机协作宣传片-无音轨.mp4')],check=True)
shots=[2,9,12.5,19,29,36.6,38.4,45.8,50.6,56,61]
board=Image.new('RGB',(1280,6*388),'#e8eef7')
for i,t in enumerate(shots):
 target=out/f'scene-{i+1:02d}.jpg'
 subprocess.run(['ffmpeg','-v','error','-y','-ss',str(t),'-i',str(video),'-frames:v','1','-vf','scale=640:360',str(target)],check=True)
 board.paste(Image.open(target),((i%2)*640,(i//2)*388))
 ImageDraw.Draw(board).text(((i%2)*640+16,(i//2)*388+366),f'{i+1:02d} / {t:04.1f}s',fill='#65738a')
board.save(out/'故事板.jpg',quality=94)
