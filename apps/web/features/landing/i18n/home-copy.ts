import type { LandingDictionaryLocale } from "./types";

type HomeCopy = {
  pause: string;
  autoPlay: string;
  skip: string;
  intro: string;
  title: string[];
  description: string;
  watch: string;
  footnote: string;
  artLabel: string;
  human: string;
  humanRole: string;
  agentRole: string;
  handoff: string;
  result: string;
  task: string;
  evidence: string;
  review: string;
  artCaption: string;
  compatibility: string;
  more: string;
  workflowLabel: string;
  workflowTitle: string[];
  workflowDescription: string;
  useCases: string;
  demoTitle: string;
  demoLabel: string;
  chooseStep: string;
  stepCount: string;
  next: string;
  restart: string;
  steps: {
    title: string;
    owner: string;
    role: string;
    status: string;
    heading: string;
    description: string;
    file: string;
    detail: string;
    message: string;
  }[];
  filmLabel: string;
  filmTitle: string[];
  filmDescription: string;
  videoLabel: string;
  filmCaption: string;
  downloadFilm: string;
  videoError: string;
  capabilitiesTitle: string[];
  capabilitiesDescription: string;
  features: { title: string; description: string }[];
  openTitle: string;
  openDescription: string;
};

const zh: HomeCopy = {
  pause: "暂停演示",
  autoPlay: "自动演示",
  skip: "跳转到正文",
  intro: "人类与 Agent 的协作空间",
  title: ["人类与 Agent，", "共创一个生态。"],
  description:
    "人明确目标，Agent 推进执行。把任务、工作 SOP 与交付证据放在一起，让每一次交接都有方向。",
  watch: "看协作如何发生",
  footnote: "开源 · 可自部署 · 连接你自己的 Agent",
  artLabel:
    "人类定义目标，Agent 执行任务，在 Silieco 中提交证据并交还给人验收。",
  human: "人类成员",
  humanRole: "定义目标 · 判断结果",
  agentRole: "理解上下文 · 推进执行",
  handoff: "目标与上下文",
  result: "产物已提交",
  task: "官网改版 · 等待验收",
  evidence: "页面预览与验证记录",
  review: "交给负责人确认",
  artCaption: "人与 Agent，共享一条工作流。",
  compatibility: "与你熟悉的 Agent 一起工作",
  more: "查看接入方式",
  workflowLabel: "把协作变成一条完整的流程",
  workflowTitle: ["知道怎么推进，", "也知道何时交接。"],
  workflowDescription:
    "用工作 SOP 明确阶段、负责人和验收条件。Agent 按规则推进，关键判断回到人手中，任务与证据一起交接。",
  useCases: "探索协作场景",
  demoTitle: "产品迭代 SOP",
  demoLabel: "交互演示",
  chooseStep: "选择工作流阶段",
  stepCount: "第 {n} 阶段 / 共 3 阶段",
  next: "下一步",
  restart: "重新体验",
  steps: [
    {
      title: "明确目标",
      owner: "产品负责人",
      role: "人类成员",
      status: "需求确认",
      heading: "先对齐，什么才算完成。",
      description:
        "将业务目标、关联资料和验收范围放进同一个任务，让执行从清晰的上下文开始。",
      file: "官网改版需求.md",
      detail: "目标 · 范围 · 验收条件",
      message: "首页需要讲清人机协作，嵌入宣传片，并适配手机。",
    },
    {
      title: "Agent 执行",
      owner: "开发 Agent",
      role: "数字员工",
      status: "执行与检查",
      heading: "按流程推进，带着结果回来。",
      description:
        "Agent 读取任务与资料，按 SOP 执行，并提交页面、文件和验证记录。",
      file: "页面预览与检查结果",
      detail: "实现产物 · 验证记录",
      message: "页面已经完成，附上预览和检查结果，请负责人验收。",
    },
    {
      title: "人类验收",
      owner: "研发负责人",
      role: "人类成员",
      status: "交接与验收",
      heading: "让最终判断，回到人手里。",
      description:
        "负责人核对交付物与验收条件，补充反馈或确认完成，过程留在项目中。",
      file: "验收结论.md",
      detail: "交付证据 · 业务反馈 · 完成记录",
      message: "已核对页面、视频与移动端体验，确认本次交付。",
    },
  ],
  filmLabel: "76 秒，看见协作的全貌",
  filmTitle: ["从一个任务，", "到一次共同交付。"],
  filmDescription:
    "人类员工与 Agent 数字员工，如何按照工作 SOP 协同推进？把这段过程，放进一支短片。",
  videoLabel: "Silieco v3 人机协作宣传片，76 秒",
  filmCaption: "协作流程演绎 · 中文 · 可开启字幕与声音",
  downloadFilm: "下载完整影片",
  videoError: "暂时无法播放，可下载完整影片观看。",
  capabilitiesTitle: ["交付留下成果，", "协作留下经验。"],
  capabilitiesDescription:
    "把分散在对话、文件和工具里的工作，组织成下一次可以继续的上下文。",
  features: [
    {
      title: "工作 SOP，让经验可复用",
      description:
        "通过流程编辑器组织阶段与分工，用对话补全草稿，把团队的工作方法沉淀下来。",
    },
    {
      title: "项目资源，让上下文不掉线",
      description:
        "按项目和任务组织文件，让资料、执行产物和讨论围绕同一项工作聚合。",
    },
    {
      title: "自有运行环境，让执行落地",
      description:
        "连接本地或远程运行环境，让 Agent 在你的工具和基础设施里完成实际工作。",
    },
  ],
  openTitle: "从真实工作开始。",
  openDescription:
    "开放源码，自主部署。让人和 Agent，在你掌握的工作空间里协作。",
};

const en: HomeCopy = {
  pause: "Pause demo",
  autoPlay: "Play demo",
  skip: "Skip to content",
  intro: "A workspace for humans and agents",
  title: ["Human experience.", "Agent capability."],
  description:
    "People set the direction. Agents move work forward. Bring tasks, working procedures, and evidence together, so every handoff has a clear next step.",
  watch: "See it in action",
  footnote: "Open source · Self-hostable · Your own agents",
  artLabel:
    "Humans define goals, agents execute, and evidence returns to a human for review through Silieco.",
  human: "Your team",
  humanRole: "Set direction · Review outcomes",
  agentRole: "Read context · Execute work",
  handoff: "Goals & context",
  result: "Deliverables submitted",
  task: "Website redesign · Ready for review",
  evidence: "Preview & verification records",
  review: "Handed to the reviewer",
  artCaption: "People and agents. One shared workflow.",
  compatibility: "Works with the agents you know",
  more: "Explore integrations",
  workflowLabel: "A complete path from intent to delivery",
  workflowTitle: ["Know the next step.", "Know when to hand off."],
  workflowDescription:
    "Define stages, owners, and acceptance criteria with SOPs. Agents follow the process; people make the key decisions. Every handoff carries its evidence.",
  useCases: "Explore use cases",
  demoTitle: "Product iteration SOP",
  demoLabel: "Interactive demo",
  chooseStep: "Choose a workflow stage",
  stepCount: "Stage {n} of 3",
  next: "Next step",
  restart: "Start again",
  steps: [
    {
      title: "Define",
      owner: "Product owner",
      role: "Human teammate",
      status: "Requirements",
      heading: "Agree on what done looks like.",
      description:
        "Keep goals, source material, and acceptance criteria in one task so execution starts with shared context.",
      file: "Website redesign brief.md",
      detail: "Goals · Scope · Acceptance criteria",
      message:
        "Explain human–agent collaboration, embed the film, and support mobile.",
    },
    {
      title: "Execute",
      owner: "Development agent",
      role: "Digital teammate",
      status: "Implementation",
      heading: "Follow the process. Return with evidence.",
      description:
        "The agent reads the task, follows the SOP, and submits deliverables with verification records.",
      file: "Preview & verification results",
      detail: "Deliverables · Verification records",
      message:
        "The page is ready. Preview and verification are attached for your review.",
    },
    {
      title: "Review",
      owner: "Engineering lead",
      role: "Human teammate",
      status: "Review & handoff",
      heading: "Keep the final judgment human.",
      description:
        "Review deliverables against the acceptance criteria, provide feedback or approve, and keep the record in the project.",
      file: "Acceptance record.md",
      detail: "Evidence · Feedback · Completion",
      message:
        "Page, video, and mobile experience reviewed. Delivery accepted.",
    },
  ],
  filmLabel: "The whole collaboration, in 76 seconds",
  filmTitle: ["From a shared task", "to a shared outcome."],
  filmDescription:
    "See how human teammates and digital workers follow an SOP, pass work along, and reach a shared delivery.",
  videoLabel: "Silieco v3 collaboration film, 76 seconds",
  filmCaption: "Illustrated workflow · Chinese · Optional subtitles and sound",
  downloadFilm: "Download the film",
  videoError: "Playback is unavailable. Download the film to watch.",
  capabilitiesTitle: ["Keep the outcome.", "Build on the experience."],
  capabilitiesDescription:
    "Turn scattered conversations, files, and tools into context your team can carry into the next task.",
  features: [
    {
      title: "SOPs that carry your experience",
      description:
        "Organize stages and responsibilities in the workflow editor. Fill drafts through conversation and preserve your team’s working methods.",
    },
    {
      title: "Project resources that stay connected",
      description:
        "Organize files by project and task, keeping source material, deliverables, and discussions close to the work.",
    },
    {
      title: "Execution on your infrastructure",
      description:
        "Connect local or remote runtimes so agents can work with your tools, in environments you control.",
    },
  ],
  openTitle: "Your team. Your way of working.",
  openDescription:
    "Open source and self-hostable. A shared workspace for humans and agents, on infrastructure you control.",
};

const ja: HomeCopy = {
  pause: "デモを一時停止",
  autoPlay: "デモを再生",
  skip: "本文へ",
  intro: "人と Agent が協働するワークスペース",
  title: ["人と Agent が、", "ともに創る。"],
  description:
    "人が目標を定め、Agent が実行を進める。タスク、SOP、成果の証拠を一つにまとめ、次の担当へ確実につなぎます。",
  watch: "協働の流れを見る",
  footnote: "オープンソース · セルフホスト · 自分の Agent",
  artLabel:
    "人が目標を定め、Agent が実行し、証拠とともに人へレビューを引き継ぎます。",
  human: "チームメンバー",
  humanRole: "目標を定義 · 結果を判断",
  agentRole: "文脈を理解 · 作業を実行",
  handoff: "目標とコンテキスト",
  result: "成果物を提出",
  task: "サイト改修 · レビュー待ち",
  evidence: "プレビューと検証記録",
  review: "担当者に確認を依頼",
  artCaption: "人と Agent が、一つのワークフローで。",
  compatibility: "使い慣れた Agent と連携",
  more: "接続方法を見る",
  workflowLabel: "意図から成果まで、一つの流れに",
  workflowTitle: ["次の一歩も、", "引き継ぐ時も明確に。"],
  workflowDescription:
    "SOP で段階、担当、受け入れ条件を定義。Agent が手順を進め、人が重要な判断を担います。引き継ぎには証拠も伴います。",
  useCases: "活用事例を見る",
  demoTitle: "製品改善 SOP",
  demoLabel: "操作デモ",
  chooseStep: "段階を選択",
  stepCount: "全 3 段階中 {n} 段階目",
  next: "次の段階",
  restart: "最初から",
  steps: [
    {
      title: "目標",
      owner: "プロダクト担当",
      role: "チームメンバー",
      status: "要件確認",
      heading: "完成の条件を、先にそろえる。",
      description:
        "目標、資料、受け入れ条件を一つのタスクに集約し、共通の文脈から実行を始めます。",
      file: "サイト改修要件.md",
      detail: "目標 · 範囲 · 受け入れ条件",
      message:
        "人と Agent の協働を説明し、動画を埋め込み、モバイルに対応してください。",
    },
    {
      title: "実行",
      owner: "開発 Agent",
      role: "デジタルメンバー",
      status: "実装と検証",
      heading: "手順に沿って、成果を持ち帰る。",
      description:
        "Agent がタスクと資料を読み、SOP に従って実行。成果物と検証記録を提出します。",
      file: "プレビューと検証結果",
      detail: "成果物 · 検証記録",
      message:
        "ページが完成しました。プレビューと検証結果の確認をお願いします。",
    },
    {
      title: "レビュー",
      owner: "開発責任者",
      role: "チームメンバー",
      status: "引き継ぎと承認",
      heading: "最終判断は、人の手に。",
      description:
        "成果物を条件と照合し、フィードバックまたは承認を行い、過程をプロジェクトに残します。",
      file: "受け入れ記録.md",
      detail: "証拠 · フィードバック · 完了記録",
      message: "ページ、動画、モバイル表示を確認し、今回の成果を承認しました。",
    },
  ],
  filmLabel: "76 秒で、協働の全体像を",
  filmTitle: ["一つのタスクから、", "ともに届ける成果へ。"],
  filmDescription:
    "人とデジタルメンバーが SOP に従い、仕事を引き継ぎ、成果にたどり着くまでを短編で紹介します。",
  videoLabel: "Silieco v3 協働紹介動画、76 秒",
  filmCaption: "協働プロセスの演出 · 中国語 · 字幕と音声を選択可能",
  downloadFilm: "動画をダウンロード",
  videoError: "再生できません。動画をダウンロードしてください。",
  capabilitiesTitle: ["成果を残し、", "経験を次につなぐ。"],
  capabilitiesDescription:
    "会話、ファイル、ツールに散らばった仕事を、次のタスクに引き継げる文脈へ。",
  features: [
    {
      title: "経験を引き継ぐ SOP",
      description:
        "エディターで段階と役割を整理し、会話で下書きを補完。チームの進め方を蓄積します。",
    },
    {
      title: "つながり続ける資料",
      description:
        "プロジェクトとタスクごとにファイルを整理。資料、成果物、議論を作業のそばに。",
    },
    {
      title: "自分の環境で実行",
      description:
        "ローカルやリモートの実行環境を接続し、自分のツールで Agent が実際の仕事を進めます。",
    },
  ],
  openTitle: "あなたのチームに、あなたの進め方を。",
  openDescription:
    "オープンソース、セルフホスト対応。自分で管理できる環境で、人と Agent が協働します。",
};

const ko: HomeCopy = {
  pause: "데모 일시정지",
  autoPlay: "데모 재생",
  skip: "본문으로 이동",
  intro: "사람과 Agent가 함께하는 작업 공간",
  title: ["사람과 Agent,", "함께 만드는 생태계."],
  description:
    "사람이 목표를 정하고 Agent가 실행합니다. 작업, SOP, 결과 증거를 한곳에 모아 다음 담당자에게 명확하게 전달하세요.",
  watch: "협업 과정 보기",
  footnote: "오픈 소스 · 자체 호스팅 · 나만의 Agent",
  artLabel:
    "사람이 목표를 정하고 Agent가 실행한 뒤 증거와 함께 사람에게 검토를 요청합니다.",
  human: "팀원",
  humanRole: "목표 설정 · 결과 판단",
  agentRole: "맥락 이해 · 작업 실행",
  handoff: "목표와 맥락",
  result: "결과물 제출됨",
  task: "웹사이트 개편 · 검토 대기",
  evidence: "미리보기와 검증 기록",
  review: "담당자에게 확인 요청",
  artCaption: "사람과 Agent, 하나의 워크플로.",
  compatibility: "익숙한 Agent와 함께 작업하세요",
  more: "연결 방법 보기",
  workflowLabel: "의도에서 결과까지 하나의 흐름",
  workflowTitle: ["다음 단계도,", "인계할 때도 명확하게."],
  workflowDescription:
    "SOP로 단계, 담당자, 승인 조건을 정하세요. Agent가 절차를 따르고 중요한 판단은 사람이 합니다. 인계에는 증거도 함께합니다.",
  useCases: "활용 사례 보기",
  demoTitle: "제품 개선 SOP",
  demoLabel: "대화형 데모",
  chooseStep: "워크플로 단계 선택",
  stepCount: "전체 3단계 중 {n}단계",
  next: "다음 단계",
  restart: "다시 시작",
  steps: [
    {
      title: "목표",
      owner: "제품 담당자",
      role: "팀원",
      status: "요구사항 확인",
      heading: "완료 기준부터 합의하세요.",
      description:
        "목표, 자료, 승인 조건을 하나의 작업에 모아 공통된 맥락에서 실행을 시작합니다.",
      file: "웹사이트 개편 요구사항.md",
      detail: "목표 · 범위 · 승인 조건",
      message:
        "사람과 Agent의 협업을 설명하고, 영상을 넣고, 모바일을 지원해주세요.",
    },
    {
      title: "실행",
      owner: "개발 Agent",
      role: "디지털 팀원",
      status: "구현 및 검증",
      heading: "절차를 따르고 결과로 돌아옵니다.",
      description:
        "Agent가 작업과 자료를 읽고 SOP에 따라 실행한 뒤 결과물과 검증 기록을 제출합니다.",
      file: "미리보기와 검증 결과",
      detail: "결과물 · 검증 기록",
      message: "페이지가 완성되었습니다. 미리보기와 검증 결과를 확인해주세요.",
    },
    {
      title: "검토",
      owner: "개발 책임자",
      role: "팀원",
      status: "인계 및 승인",
      heading: "최종 판단은 사람에게.",
      description:
        "결과물을 승인 조건과 비교해 피드백하거나 완료를 승인하고, 과정을 프로젝트에 남깁니다.",
      file: "승인 기록.md",
      detail: "증거 · 피드백 · 완료 기록",
      message: "페이지, 영상, 모바일 경험을 확인하고 이번 결과를 승인했습니다.",
    },
  ],
  filmLabel: "76초로 보는 협업의 전체 과정",
  filmTitle: ["하나의 작업에서", "함께 만든 결과로."],
  filmDescription:
    "사람과 디지털 팀원이 SOP를 따라 작업을 인계하고 함께 결과를 만드는 과정을 짧은 영상으로 만나보세요.",
  videoLabel: "Silieco v3 협업 소개 영상, 76초",
  filmCaption: "협업 과정 연출 · 중국어 · 자막과 소리 선택 가능",
  downloadFilm: "영상 다운로드",
  videoError: "재생할 수 없습니다. 영상을 다운로드해 주세요.",
  capabilitiesTitle: ["결과를 남기고,", "경험을 이어가세요."],
  capabilitiesDescription:
    "대화, 파일, 도구에 흩어진 작업을 다음 작업으로 이어갈 수 있는 맥락으로 만드세요.",
  features: [
    {
      title: "경험을 담는 SOP",
      description:
        "편집기에서 단계와 역할을 정리하고 대화로 초안을 보완해 팀의 작업 방식을 쌓아갑니다.",
    },
    {
      title: "맥락이 연결되는 자료",
      description:
        "프로젝트와 작업별로 파일을 정리해 원본 자료, 결과물, 논의를 작업 가까이에 둡니다.",
    },
    {
      title: "내 인프라에서 실행",
      description:
        "로컬 또는 원격 실행 환경을 연결해 내가 관리하는 도구로 Agent가 실제 작업을 수행합니다.",
    },
  ],
  openTitle: "당신의 팀, 당신의 작업 방식.",
  openDescription:
    "오픈 소스와 자체 호스팅. 직접 관리하는 작업 공간에서 사람과 Agent가 협업합니다.",
};

export const homeCopy: Record<LandingDictionaryLocale, HomeCopy> = {
  zh,
  en,
  ja,
  ko,
};
