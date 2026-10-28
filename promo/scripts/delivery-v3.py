import csv,json,shutil,subprocess,zipfile
from pathlib import Path
root=Path(__file__).resolve().parent.parent
out=root/'out/v3'
segments=[
 (0,5,'在 Silieco 中，人类员工与 Agent 数字员工共同协作。'),
 (5,10,'产品成员输入需求，补齐业务目标、原始资料和验收范围。'),
 (10,14,'人类员工明确需求，数字员工按 SOP 参与执行。'),
 (14,19,'数字员工可以运行在另一台机器上，自动获取任务与关联信息。'),
 (19,24,'关联资料、工作 SOP 和 Skill，让数字员工知道做什么、怎么做。'),
 (24,29,'工作 SOP 明确执行边界，指导数字员工按阶段推进任务。'),
 (29,34,'达到交接条件后，数字员工提交产物，并找到对应的人类负责人。'),
 (34,38,'需要人工确认时，数字员工将任务交给指定的研发负责人。'),
 (38,42,'任务与证据一起到达，数字员工等待研发确认验收结果。'),
 (42,47,'研发核对交付结果，Agent 补充证据，产品成员确认业务范围。'),
 (47,53,'确认结果后，由研发完成验收，并将结论留在项目里。'),
 (53,59,'整个协作过程都保留在项目中，人类员工与数字员工共享完整上下文。'),
 (59,65,'从研发流程，扩展到内容生产、客户服务和数据分析等不同的工作 SOP。'),
 (65,71,'不同的人类员工和 Agent 数字员工，在同一生态中按角色、规则和边界协作。'),
 (71,76,'Silieco，让数字员工知道如何推进，何时交给谁。')]
def stamp(sec):
 ms=round(sec*1000);return f'{ms//3600000:02d}:{ms//60000%60:02d}:{ms//1000%60:02d},{ms%1000:03d}'
captions=[{'text':s,'startMs':a*1000,'endMs':b*1000,'timestampMs':None,'confidence':None} for a,b,s in segments]
(out/'讲解字幕.json').write_text(json.dumps(captions,ensure_ascii=False,indent=2))
(out/'讲解字幕.srt').write_text('\n\n'.join(f'{i+1}\n{stamp(a)} --> {stamp(b)}\n{s}' for i,(a,b,s) in enumerate(segments))+'\n')
with (out/'配音时间轴.csv').open('w',encoding='utf-8-sig') as f:
 w=csv.writer(f);w.writerow(['开始秒','结束秒','配音文案']);w.writerows(segments)
(out/'配音文案.md').write_text('# 配音文案与时间轴\n\n'+'\n\n'.join(f'**{stamp(a)[:-4]}–{stamp(b)[:-4]}**\n\n{s}' for a,b,s in segments)+'\n')
shutil.copy2(root/'public/v3/audio-timeline.json',out/'音效卡点时间轴.json')
for source,target in [('music.wav','背景音乐.wav'),('sfx.wav','提示音效.wav')]:shutil.copy2(root/'public/v3'/source,out/target)
(out/'预览.html').write_text('''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>Silieco 人机协作宣传片</title><style>body{margin:0;padding:40px;background:#f1f5fa;color:#202b3d;font:16px system-ui}main{max-width:1280px;margin:auto}video{width:100%;border-radius:18px;box-shadow:0 20px 70px #35507120}p{color:#78869a}a{color:#3978f6}</style><main><h1>Silieco · 人类员工与 Agent 数字员工协作</h1><p>76 秒 / 1080p / 30fps / 背景音乐 + 动作提示音</p><video controls preload="metadata" src="Silieco-人机协作宣传片.mp4"></video><p>人类员工与 Agent 数字员工，在 Silieco 中按照工作 SOP 协同推进。协作流程演绎，未录制真实成员或操作项目数据。</p><p><a href="讲解字幕.srt">配音字幕</a> · <a href="配音时间轴.csv">配音时间轴</a> · <a href="音效卡点时间轴.json">音效卡点</a></p></main></html>''')
