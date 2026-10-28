import json,csv
from pathlib import Path
root=Path(__file__).resolve().parent.parent
segments=[(0,4,'Silieco，让人与 Agent 一起，把项目向前推进。'),(4,10,'通过四个 Stage 的研发 SOP，连接需求整理、研发设计、任务实现和测试验收。'),(10,16,'从良品库盘点需求开始，保留原始文件，整理业务范围与验收标准。'),(16,22,'绑定项目 Skill，将项目上下文、阶段要求和专业能力一起交给 Agent。'),(22,29,'将 Task 指派给 Sili Agent，执行任务、回写结果，再推进到下一个阶段。'),(29,35,'Agent 负责执行和交付，人负责范围确认与关键验收。'),(35,42,'需求、设计、任务和验收产物留在项目目录，成为下一轮工作的上下文。'),(42,46,'Silieco，让协作有流程，让推进有上下文。')]
def stamp(s):return f'00:{s//60:02d}:{s%60:02d},000'
(root/'out/讲解字幕.srt').write_text('\n\n'.join(f'{i+1}\n{stamp(a)} --> {stamp(b)}\n{s}' for i,(a,b,s) in enumerate(segments))+'\n')
with (root/'out/字幕时间轴.csv').open('w',encoding='utf-8-sig') as f:
 w=csv.writer(f);w.writerow(['开始秒','结束秒','配音文案']);w.writerows(segments)
(root/'out/字幕时间轴.json').write_text(json.dumps([dict(start=a,end=b,text=s) for a,b,s in segments],ensure_ascii=False,indent=2))
p=root/'package-lock.json';v=json.loads(p.read_text());v['name']='silieco-product-film';v['packages']['']['name']='silieco-product-film';p.write_text(json.dumps(v,indent=2)+'\n')
