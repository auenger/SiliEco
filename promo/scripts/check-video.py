"""Deterministic post-export checks for isolated flash frames and decode integrity."""
import subprocess,json,math,sys,argparse
from pathlib import Path
parser=argparse.ArgumentParser()
parser.add_argument('video',type=Path)
parser.add_argument('--expected-frames',type=int,default=840)
args=parser.parse_args()
video=args.video;w,h=160,90;size=w*h
r=subprocess.run(['ffprobe','-v','error','-show_entries','format=duration,size:stream=codec_name,width,height,r_frame_rate,sample_rate,channels','-of','json',str(video)],capture_output=True,text=True,check=True)
meta=json.loads(r.stdout)
p=subprocess.Popen(['ffmpeg','-v','error','-i',str(video),'-an','-vf',f'scale={w}:{h},format=gray','-f','rawvideo','-'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
frames=[]
while True:
    data=p.stdout.read(size)
    if not data:break
    if len(data)!=size:raise RuntimeError('Incomplete decoded frame')
    frames.append(data)
err=p.stderr.read().decode();code=p.wait()
if code or err:raise RuntimeError(err)
def diff(a,b):return sum(abs(x-y) for x,y in zip(a,b))/size
means=[sum(a)/size for a in frames]
diffs=[0]+[diff(frames[i-1],frames[i]) for i in range(1,len(frames))]
suspects=[]
for i in range(1,len(frames)-1):
    before,after=diffs[i],diffs[i+1]
    if min(before,after)>1.0:
        across=diff(frames[i-1],frames[i+1])
        if across<min(before,after)*.45:suspects.append({'frame':i,'time':round(i/30,3),'before_difference':round(before,4),'after_difference':round(after,4),'neighbor_difference':round(across,4)})
flat=[]
for i,a in enumerate(frames):
    if max(a)-min(a)<8:flat.append(i)
result={'video':str(video),'metadata':meta,'decoded_frames':len(frames),'expected_frames':args.expected_frames,'decode_errors':[],'flat_blank_frames':flat,'isolated_flash_candidates':suspects,'mean_luma_range':[round(min(means),3),round(max(means),3)],'largest_adjacent_difference':round(max(diffs),3),'method':'160x90 grayscale; single-frame reversal test (both neighbor differences >1/255, neighbor-to-neighbor difference <45% of each); flat-frame range <8/255; full stream decode','limits':'Heuristic for isolated single-frame flashes; keyframes and scene boundaries also require visual review.'}
out=video.with_suffix('.quality.json');out.write_text(json.dumps(result,indent=2));print(json.dumps(result,indent=2))
if len(frames)!=args.expected_frames or flat or suspects:sys.exit(2)
