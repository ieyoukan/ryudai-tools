import { Suspense, lazy, useEffect, type ComponentType } from 'react'
import { Route, Switch, useLocation } from 'wouter'

import { AppShell } from '@/components/AppShell'
import { LoadingBlock } from '@/components/page'
import { retryImport } from '@/lib/stale-build'

// ページごとに分けて読み込む（スマホで最初の表示を軽くする）
// デプロイ後に前の版の画面から移動して読み込めなかったときは、再読み込みして新しい版にする（lib/stale-build）
const page = <K extends string>(load: () => Promise<Record<K, ComponentType>>, name: K) => lazy(retryImport(() => load().then((m) => ({ default: m[name] }))))
const LoginPage = page(() => import('@/pages/LoginPage'), 'LoginPage')
const CoursesPage = page(() => import('@/pages/CoursesPage'), 'CoursesPage')
const ContentsPage = page(() => import('@/pages/ContentsPage'), 'ContentsPage')
const FilesPage = page(() => import('@/pages/FilesPage'), 'FilesPage')
const RegistrationPage = page(() => import('@/pages/RegistrationPage'), 'RegistrationPage')
const CandidatesPage = page(() => import('@/pages/CandidatesPage'), 'CandidatesPage')
const GradesPage = page(() => import('@/pages/grades/GradesPage'), 'GradesPage')
const PlanPage = page(() => import('@/pages/plan/PlanPage'), 'PlanPage')
const SyllabusPage = page(() => import('@/pages/SyllabusPage'), 'SyllabusPage')
const LecturePage = page(() => import('@/pages/LecturePage'), 'LecturePage')
const SyllabusDetailPage = page(() => import('@/pages/SyllabusDetailPage'), 'SyllabusDetailPage')
const MessagesPage = page(() => import('@/pages/MessagesPage'), 'MessagesPage')
const SettingsPage = page(() => import('@/pages/SettingsPage'), 'SettingsPage')
const TermsPage = page(() => import('@/pages/PolicyPages'), 'TermsPage')
const PrivacyPage = page(() => import('@/pages/PolicyPages'), 'PrivacyPage')
const DownloadsPage = page(() => import('@/pages/DownloadsPage'), 'DownloadsPage')
const NotFoundPage = page(() => import('@/pages/NotFoundPage'), 'NotFoundPage')

const titles: [RegExp, string][] = [
  [/^\/login/, 'ログイン'],
  [/^\/courses\/[^/]+\/contents/, '資料'],
  [/^\/courses\/[^/]+/, 'コース'],
  [/^\/courses/, '時間割'],
  [/^\/registration\/candidates/, '入れられる講義'],
  [/^\/registration/, '履修登録'],
  [/^\/grades/, '成績・卒業要件'],
  [/^\/plan/, '卒業までの計画'],
  [/^\/lectures\//, '講義'],
  [/^\/syllabus\/\d+/, 'シラバス'],
  [/^\/syllabus/, 'シラバス検索'],
  [/^\/messages/, 'メッセージ'],
  [/^\/settings/, '設定'],
  [/^\/terms/, '利用規約'],
  [/^\/privacy/, 'プライバシーポリシー'],
  [/^\/downloads/, 'ダウンロード'],
]

export default function App() {
  const [path] = useLocation()
  useEffect(() => {
    const t = titles.find(([re]) => re.test(path))?.[1]
    document.title = t ? `${t} - Ryudai Tools` : 'Ryudai Tools'
    window.scrollTo(0, 0)
  }, [path])

  return (
    <AppShell>
      <Suspense fallback={<LoadingBlock />}>
      <Switch>
        <Route path="/login" component={LoginPage} />
        <Route path="/courses" component={CoursesPage} />
        <Route path="/courses/:courseID" component={ContentsPage} />
        <Route path="/courses/:courseID/contents/:contentID" component={FilesPage} />
        <Route path="/registration" component={RegistrationPage} />
        <Route path="/registration/candidates" component={CandidatesPage} />
        <Route path="/grades" component={GradesPage} />
        <Route path="/plan" component={PlanPage} />
        <Route path="/syllabus" component={SyllabusPage} />
        <Route path="/syllabus/:year/:code" component={SyllabusDetailPage} />
        <Route path="/lectures/:year/:code" component={LecturePage} />
        <Route path="/messages" component={MessagesPage} />
        <Route path="/settings" component={SettingsPage} />
        <Route path="/terms" component={TermsPage} />
        <Route path="/privacy" component={PrivacyPage} />
        <Route path="/downloads" component={DownloadsPage} />
        <Route component={NotFoundPage} />
      </Switch>
      </Suspense>
    </AppShell>
  )
}
