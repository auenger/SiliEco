import React from 'react';
import {Img,staticFile,interpolate,Easing} from 'remotion';

const C={ink:'#202b3d',muted:'#78869a',line:'#d9e3f0',blue:'#3978f6'};
const clamp={extrapolateLeft:'clamp' as const,extrapolateRight:'clamp' as const};
const curve=Easing.bezier(.22,.8,.24,1);
const p=(t:number,a:number,d=.7)=>interpolate(t,[a,a+d],[0,1],{...clamp,easing:curve});
type Member={file:string;role:string;kind:'human'|'agent';zoom?:number};
type Flow={name:string;color:string;x:number;y:number;stages:string[];members:Member[]};
const flows:Flow[]=[
 {name:'系统研发 SOP',color:'#3978f6',x:100,y:300,stages:['需求整理','研发实现','测试验收'],members:[{file:'product',role:'产品',kind:'human',zoom:1.2},{file:'agent4',role:'研发 Agent',kind:'agent',zoom:1.05},{file:'developer',role:'研发负责人',kind:'human',zoom:1.2}]},
 {name:'内容生产 SOP',color:'#9470c7',x:1210,y:300,stages:['创意策划','内容制作','发布确认'],members:[{file:'human1',role:'内容策划',kind:'human',zoom:1.1},{file:'agent1',role:'内容 Agent',kind:'agent',zoom:1.4},{file:'human2',role:'内容负责人',kind:'human',zoom:1.15}]},
 {name:'客户服务 SOP',color:'#238a76',x:100,y:650,stages:['问题受理','分析处理','服务确认'],members:[{file:'human3',role:'客户服务',kind:'human',zoom:1.35},{file:'agent2',role:'服务 Agent',kind:'agent',zoom:1.25},{file:'human4',role:'业务专家',kind:'human',zoom:1.1}]},
 {name:'数据分析 SOP',color:'#bf8742',x:1210,y:650,stages:['业务提问','数据分析','结论确认'],members:[{file:'human2',role:'业务分析',kind:'human',zoom:1.15},{file:'agent3',role:'分析 Agent',kind:'agent',zoom:1.4},{file:'human1',role:'业务负责人',kind:'human',zoom:1.1}]},
];

function Portrait({member,color,active}:{member:Member;color:string;active:boolean}){
 return <div style={{width:62,height:62,position:'relative',borderRadius:member.kind==='human'?99:17,border:`2px solid ${active?color:'#fff'}`,background:'white',boxShadow:active?`0 0 0 5px ${color}16`:'0 3px 12px #29466b10'}}>
  <div style={{position:'absolute',inset:2,overflow:'hidden',borderRadius:member.kind==='human'?99:14}}><Img src={staticFile(`avatars/${member.file}.jpg`)} style={{width:'100%',height:'100%',objectFit:'cover',scale:member.zoom??1}}/></div>
  <span style={{position:'absolute',right:-4,bottom:-4,width:19,height:19,border:'3px solid white',borderRadius:member.kind==='human'?99:6,background:member.kind==='human'?'#9470c7':C.blue}}/>
 </div>;
}

function Workflow({flow,index,t}:{flow:Flow;index:number;t:number}){
 const reveal=index===0?1:p(t,1.2+index*.3,.7);
 const run=Math.max(0,t-3.5-index*.5);
 const phase=(run%5.5)/5.5;
 const active=phase<.23?0:phase<.72?1:2;
 const step=p(phase,.12,.73);
 return <div style={{position:'absolute',left:flow.x,top:flow.y,width:610,height:240,background:'white',border:`1px solid ${flow.color}38`,borderRadius:23,padding:24,boxShadow:'0 16px 40px #29466b0b',opacity:reveal,translate:`0px ${28*(1-reveal)}px`}}>
  <div style={{display:'flex',alignItems:'center',justifyContent:'space-between'}}><div style={{fontSize:25,fontWeight:600,display:'flex',alignItems:'center',gap:11}}><span style={{width:9,height:24,background:flow.color,borderRadius:5}}/>{flow.name}</div><div style={{fontSize:15,color:flow.color}}>● 协作进行中</div></div>
  <svg width="610" height="180" style={{position:'absolute',left:0,top:53}}><path d="M103 80H507" stroke={C.line} strokeWidth="2"/><path d="M103 80H507" pathLength="1" stroke={flow.color} strokeWidth="2" strokeDasharray="1" strokeDashoffset={1-step}/><circle cx={103+step*404} cy="80" r="5" fill={flow.color}/></svg>
  {flow.members.map((member,i)=><div key={i} style={{position:'absolute',left:32+i*202,top:75,width:142,display:'flex',alignItems:'center',flexDirection:'column'}}>
   <div style={{fontSize:16,color:C.muted,marginBottom:12}}>{flow.stages[i]}</div>
   <Portrait member={member} color={flow.color} active={active===i&&run>0}/>
   <div style={{fontSize:18,fontWeight:550,marginTop:9}}>{member.role}</div>
   <div style={{fontSize:13,color:member.kind==='human'?'#9470c7':C.blue,marginTop:3}}>{member.kind==='human'?'人类员工':'数字员工'}</div>
  </div>)}
  <div style={{position:'absolute',left:85+step*404,top:83,opacity:run>0?Math.sin(Math.PI*step)*.9:0,background:flow.color,color:'white',fontSize:12,borderRadius:5,padding:'3px 7px'}}>Task</div>
 </div>;
}

export function EcosystemEnding({t}:{t:number}){
 const q=t-59;
 const zoom=interpolate(q,[0,1,3.8],[1.45,1.45,1],{...clamp,easing:curve});
 const cx=interpolate(q,[0,1,3.8],[405,405,960],{...clamp,easing:curve});
 const cy=interpolate(q,[0,1,3.8],[420,420,580],{...clamp,easing:curve});
 const finish=p(q,12,.7);
 return <>
  <div style={{position:'absolute',left:90,top:112,opacity:1-finish}}><div style={{fontSize:17,letterSpacing:2,color:C.blue,marginBottom:10}}>07 / 从一个项目，扩展到多种工作流程</div><div style={{fontSize:50,fontWeight:600,letterSpacing:-1.5}}>多条工作 SOP，连接整个协作生态。</div><div style={{fontSize:23,color:C.muted,marginTop:13}}>不同的人类员工与 Agent 数字员工，按角色、规则和边界共同协作。</div></div>
  <div style={{position:'absolute',left:90,top:112,opacity:finish,translate:`0px ${12*(1-finish)}px`}}><div style={{fontSize:17,letterSpacing:2,color:C.blue,marginBottom:10}}>SILIECO / HUMAN + AGENT</div><div style={{fontSize:50,fontWeight:600,letterSpacing:-1.5}}>人类员工与数字员工，共同工作的生态。</div><div style={{fontSize:23,color:C.muted,marginTop:13}}>让数字员工知道如何推进、边界在哪里，以及何时交给谁确认。</div></div>
  <div style={{position:'absolute',inset:'270px 0 150px',overflow:'hidden'}}>
   <div style={{position:'absolute',left:960-cx*zoom,top:580-cy*zoom-270,width:1920,height:1080,scale:zoom,transformOrigin:'0 0'}}>
    <svg width="1920" height="1080" style={{position:'absolute',opacity:p(q,1.1)}}>{flows.map((flow,i)=>{
      const left=i%2===0;
      const x1=left?810:1110,y1=590,x2=left?710:1210,y2=flow.y+120;
      const d=`M${x1} ${y1}C${left?760:1160} ${y1} ${left?770:1150} ${y2} ${x2} ${y2}`;
      const packet=(Math.max(0,q-3-i*.45)%4)/4;
      const x=(1-packet)**3*x1+3*(1-packet)**2*packet*(left?760:1160)+3*(1-packet)*packet**2*(left?770:1150)+packet**3*x2;
      const y=(1-packet)**3*y1+3*(1-packet)**2*packet*y1+3*(1-packet)*packet**2*y2+packet**3*y2;
      return <g key={flow.name}><path d={d} fill="none" stroke={flow.color+'38'} strokeWidth="2"/><path d={d} fill="none" stroke={flow.color} strokeWidth="2" pathLength="1" strokeDasharray="1" strokeDashoffset={1-p(q,1.5+i*.3,1)}/><circle cx={x} cy={y} r="4.5" fill={flow.color} opacity={p(q,3)}/></g>;
    })}</svg>
    {flows.map((flow,i)=><Workflow key={flow.name} flow={flow} index={i} t={q}/>)}
    <div style={{position:'absolute',left:810,top:509,width:300,height:162,display:'flex',flexDirection:'column',alignItems:'center',justifyContent:'center',background:'#fff',border:`1px solid ${C.blue}38`,borderRadius:27,boxShadow:'0 15px 45px #3978f616',opacity:p(q,1.6),scale:1+.025*finish}}>
     <div style={{display:'flex',alignItems:'center',gap:13}}><Img src={staticFile('logo.svg')} style={{width:52,height:52,borderRadius:12}}/><span style={{fontSize:41,fontWeight:650,letterSpacing:-1.5}}>Silieco</span></div>
     <div style={{fontSize:18,color:C.muted,marginTop:14}}>项目 · 任务 · Skill · 上下文</div>
    </div>
   </div>
  </div>
  <div style={{position:'absolute',left:0,right:0,top:936,textAlign:'center',opacity:p(q,4)}}><div style={{display:'flex',gap:12,justifyContent:'center',fontSize:20}}>{['工作目标','执行边界','交接条件','确认人'].map(x=><span key={x} style={{background:'white',border:`1px solid ${C.line}`,padding:'12px 22px',borderRadius:11,color:C.ink}}>{x}</span>)}</div><div style={{fontSize:16,color:C.muted,marginTop:18}}>工作 SOP 示例　·　github.com/auenger/SiliEco</div></div>
 </>;
}
