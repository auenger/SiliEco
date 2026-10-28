from pathlib import Path
import shutil,subprocess
root=Path(__file__).resolve().parent.parent
out=root/'out/v3'
video=out/'Silieco-人机协作宣传片.mp4'
base=out/'base-64s.mp4'
if not base.exists():shutil.copy2(video,base)
target=out/'assembled.mp4'
subprocess.run(['ffmpeg','-hide_banner','-y','-i',str(base),'-i',str(out/'ecosystem-ending.mp4'),'-i',str(root/'public/v3/music.wav'),'-i',str(root/'public/v3/sfx.wav'),'-filter_complex','[0:v]trim=end_frame=1770,setpts=PTS-STARTPTS[v0];[1:v]trim=end_frame=510,setpts=PTS-STARTPTS[v1];[v0][v1]concat=n=2:v=1:a=0[v];[2:a][3:a]amix=inputs=2:normalize=0[a]','-map','[v]','-map','[a]','-frames:v','2280','-r','30','-c:v','libx264','-preset','medium','-crf','17','-pix_fmt','yuv420p','-c:a','aac','-b:a','192k','-movflags','+faststart',str(target)],check=True)
target.replace(video)
