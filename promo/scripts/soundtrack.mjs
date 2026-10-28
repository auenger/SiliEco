import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const sr=48000,duration=64,N=sr*duration;
const music=[new Float64Array(N),new Float64Array(N)];
const fx=[new Float64Array(N),new Float64Array(N)];
let seed=89123;
const noise=()=>{seed=(seed*16807)%2147483647;return seed/1073741823.5-1;};
const hz=n=>440*2**((n-69)/12);
function add(track,at,dur,fn,amp=1,pan=0){const off=Math.round(at*sr),len=Math.floor(dur*sr);for(let i=0;i<len&&off+i<N;i++){if(off+i<0)continue;const t=i/sr,v=fn(t,i)*amp;track[0][off+i]+=v*(1-pan*.45);track[1][off+i]+=v*(1+pan*.45);}}
// Original, restrained 120 BPM instrumental: warm keys, soft pulse, sparse melody.
const chords=[[50,57,61,64,69],[47,54,57,61,66],[43,50,54,57,62],[45,52,57,59,64]];
for(let bar=0;bar<32;bar++){
 const notes=chords[Math.floor(bar/2)%4],at=bar*2;
 for(let k=0;k<notes.length;k++){const freq=hz(notes[k]);add(music,at,3,t=>{const env=Math.min(1,t/.3)*Math.min(1,(3-t)/1.1);return env*(Math.sin(2*Math.PI*freq*t)+.2*Math.sin(2*Math.PI*freq*1.001*t));},.014,(k-2)/2);}
 if(bar>=2&&bar<30){for(let b=0;b<4;b++){add(music,at+b*.5,.35,t=>Math.sin(2*Math.PI*(47*t+65*.025*(1-Math.exp(-t/.025))))*Math.exp(-t*15),.09);const freq=hz(notes[0]-12);add(music,at+b*.5,.4,t=>Math.sin(2*Math.PI*freq*t)*Math.min(1,t/.014)*Math.exp(-t*9),.045);}
 for(let h=0;h<8;h++)add(music,at+h*.25,.045,t=>noise()*Math.exp(-t*110),h%2?.008:.004,h%2?.6:-.6);
 for(const b of [1,3])add(music,at+b*.5,.12,t=>(noise()*.7+Math.sin(2*Math.PI*180*t)*.3)*Math.exp(-t*40),.018,.1);
 }
 if(bar>=1&&bar<31){for(let k=0;k<4;k++){const freq=hz(notes[[2,3,1,4][k]]+12);const tone=t=>Math.sin(2*Math.PI*freq*t)*Math.exp(-t*6)*Math.min(1,t/.01)+.07*Math.sin(4*Math.PI*freq*t)*Math.exp(-t*15);add(music,at+k*.5,.9,tone,.025,k%2?.5:-.5);add(music,at+k*.5+.375,.9,tone,.007,k%2?-.6:.6);}}
}
const cues=[];
function cue(at,type,label){cues.push({time:at,type,label});if(type==='click'){add(fx,at,.05,t=>(noise()*.25+Math.sin(2*Math.PI*1500*t)) *Math.exp(-t*100),.10);}else if(type==='receive'){[81,85,88].forEach((n,i)=>{const freq=hz(n);add(fx,at+i*.09,.8,t=>Math.sin(2*Math.PI*freq*t)*Math.exp(-t*8)*Math.min(1,t/.004),.08,i*.2-.2);});}else if(type==='success'){[74,78,81,86].forEach((n,i)=>{const freq=hz(n);add(fx,at+i*.12,1.4,t=>Math.sin(2*Math.PI*freq*t)*Math.exp(-t*4.5)*Math.min(1,t/.004),.07,i*.15-.2);});}else{add(fx,at,.65,t=>noise()*.6*Math.sin(Math.PI*t/.65)**2,.027);add(fx,at,.55,t=>Math.sin(2*Math.PI*(320*t+350*t*t))*Math.sin(Math.PI*t/.55)**2,.025);}}
[5,14,24,34,42,53,59].forEach(t=>cue(t-.15,'swoosh','场景转场'));
cue(11.8,'click','产品确认并交接');cue(12.8,'receive','Agent 成为负责人');cue(14.6,'swoosh','任务跨机器传递');cue(16.2,'receive','运行时获取关联信息');cue(20,'click','SOP 与 Skill 已就绪');cue(27.2,'click','推进研发实现');cue(31.2,'receive','实现与本地检查完成');cue(36.1,'swoosh','Agent 向研发移交任务');cue(38,'receive','研发接收验收任务');cue(44.4,'click','Agent 回答验收问题');cue(50,'success','研发确认验收');cue(59.4,'success','品牌收尾');
function write(track,name,fadeMusic=false){const data=Buffer.alloc(N*4+44);data.write('RIFF');data.writeUInt32LE(N*4+36,4);data.write('WAVEfmt ',8);data.writeUInt32LE(16,16);data.writeUInt16LE(1,20);data.writeUInt16LE(2,22);data.writeUInt32LE(sr,24);data.writeUInt32LE(sr*4,28);data.writeUInt16LE(4,32);data.writeUInt16LE(16,34);data.write('data',36);data.writeUInt32LE(N*4,40);let peak=0,sum=0;for(let i=0;i<N;i++){const t=i/sr,fade=Math.max(0,Math.min(1,t/.2,(duration-t)/2));const duck=fadeMusic&&[11.8,16.2,38,50,59.4].some(a=>t>a&&t<a+1)?.72:1;for(let ch=0;ch<2;ch++){const x=Math.max(-.95,Math.min(.95,track[ch][i]*fade*duck));peak=Math.max(peak,Math.abs(x));sum+=x*x;data.writeInt16LE(Math.round(x*32767),44+i*4+ch*2);}}fs.writeFileSync(path.join(root,'public/v2',name),data);return {name,peakDb:20*Math.log10(peak),rmsDb:20*Math.log10(Math.sqrt(sum/(N*2)))};}
const metadata={duration,bpm:120,sampleRate:sr,channels:2,source:'Original procedural composition and synthesized UI cues; no external samples',tracks:[write(music,'music.wav',true),write(fx,'sfx.wav')],cues};
fs.writeFileSync(path.join(root,'public/v2/audio-timeline.json'),JSON.stringify(metadata,null,2));console.log(JSON.stringify(metadata,null,2));
