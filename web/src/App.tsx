import React, { useEffect, useState } from 'react';
import {
  ChevronDown,
  Database,
  Download,
  Eye,
  FilePlus2,
  KeyRound,
  Layers,
  LogOut,
  PanelRight,
  Plus,
  Table2,
  Terminal,
  Upload,
  Wrench,
  X,
} from 'lucide-react';
import { api } from './api/client';
import { BrandLogo } from './components/BrandLogo';
import { ConnectionModal } from './components/ConnectionModal';
import { DataGridTab } from './components/DataGridTab';
import { ImportExportModal } from './components/ImportExportModal';
import { LoginModal } from './components/LoginModal';
import { ObjectBrowserPane, type ObjectCategory } from './components/ObjectBrowserPane';
import { RightInfoPane } from './components/RightInfoPane';
import { SidebarTree, type SelectedTreeContext } from './components/SidebarTree';
import { SqlWorkspace } from './components/SqlWorkspace';
import { StructureDesigner } from './components/StructureDesigner';
import type {
  ConnectionView,
  DatabaseObject,
  EngineCapabilities,
  EngineType,
  WorkbenchTab,
} from './types/db';

const OBJECTS_TAB_ID = '__objects__';

const ENGINE_MENU_ITEMS: Array<{ engine: EngineType; label: string; badge: string }> = [
  { engine: 'mysql', label: 'MySQL / MariaDB', badge: '3306' },
  { engine: 'postgres', label: 'PostgreSQL', badge: '5432' },
  { engine: 'sqlite', label: 'SQLite 本地库', badge: 'File' },
  { engine: 'sqlserver', label: 'SQL Server', badge: '1433' },
  { engine: 'oracle', label: 'Oracle Database', badge: '1521' },
  { engine: 'dameng', label: '达梦数据库 (DM8)', badge: '5236' },
];

export default function App() {
  const [authLoading, setAuthLoading] = useState(true);
  const [authEnabled, setAuthEnabled] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [currentUser, setCurrentUser] = useState('');

  const [connections, setConnections] = useState<ConnectionView[]>([]);
  const [capabilities, setCapabilities] = useState<Record<EngineType, EngineCapabilities>>(
    {} as Record<EngineType, EngineCapabilities>,
  );

  // Do NOT auto-select or auto-query any connection on initial load
  const [selectedContext, setSelectedContext] = useState<SelectedTreeContext | null>(null);
  const [selectedObject, setSelectedObject] = useState<DatabaseObject | null>(null);
  const [objectCategory, setObjectCategory] = useState<ObjectCategory>('tables');
  const [objectsTabOpen, setObjectsTabOpen] = useState(false);
  const [tabs, setTabs] = useState<WorkbenchTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string>('');

  const [connMenuOpen, setConnMenuOpen] = useState(false);
  const [connModalOpen, setConnModalOpen] = useState(false);
  const [initialModalEngine, setInitialModalEngine] = useState<EngineType | undefined>(undefined);
  const [editingConn, setEditingConn] = useState<ConnectionView | null>(null);
  const [showRightPane, setShowRightPane] = useState(false);
  const [refreshSignal, setRefreshSignal] = useState(0);
  const [globalIoModal, setGlobalIoModal] = useState<'import' | 'export' | null>(null);

  useEffect(() => {
    const close = () => setConnMenuOpen(false);
    window.addEventListener('click', close);
    return () => window.removeEventListener('click', close);
  }, []);

  const loadWorkspaceData = async () => {
    const [capsRes, connsRes] = await Promise.all([api.capabilities(), api.listConnections()]);
    const capMap = {} as Record<EngineType, EngineCapabilities>;
    for (const c of capsRes.engines || []) {
      capMap[c.engine] = c;
    }
    setCapabilities(capMap);
    setConnections(connsRes.connections || []);
  };

  useEffect(() => {
    api
      .authStatus()
      .then(async (res) => {
        setAuthEnabled(res.authEnabled);
        setAuthenticated(res.authenticated);
        setCurrentUser(res.username || '');
        if (res.authenticated) {
          await loadWorkspaceData();
        }
      })
      .catch(() => {
        setAuthenticated(false);
      })
      .finally(() => {
        setAuthLoading(false);
      });
  }, []);

  const handleLoginSuccess = async (user: string) => {
    setAuthenticated(true);
    setCurrentUser(user);
    await loadWorkspaceData();
  };

  const handleLogout = async () => {
    await api.logout();
    setAuthenticated(false);
    setTabs([]);
    setObjectsTabOpen(false);
    setActiveTabId('');
  };

  const openNewConnectionModal = (engine?: EngineType) => {
    setEditingConn(null);
    setInitialModalEngine(engine);
    setConnModalOpen(true);
  };

  const openTab = (tab: WorkbenchTab) => {
    setTabs((prev) => {
      const existing = prev.find((t) => t.id === tab.id);
      if (existing) return prev;
      return [...prev, tab];
    });
    setActiveTabId(tab.id);
  };

  const closeTab = (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (id === OBJECTS_TAB_ID) {
      setObjectsTabOpen(false);
      if (activeTabId === OBJECTS_TAB_ID) {
        setActiveTabId(tabs.length > 0 ? tabs[tabs.length - 1].id : '');
      }
      return;
    }
    setTabs((prev) => {
      const next = prev.filter((t) => t.id !== id);
      if (activeTabId === id) {
        if (next.length > 0) {
          setActiveTabId(next[next.length - 1].id);
        } else if (objectsTabOpen) {
          setActiveTabId(OBJECTS_TAB_ID);
        } else {
          setActiveTabId('');
        }
      }
      return next;
    });
  };

  const openObjectsOverview = (cat: ObjectCategory, ctx?: SelectedTreeContext) => {
    if (ctx) {
      setSelectedContext(ctx);
    } else if (!selectedContext && connections.length > 0) {
      setSelectedContext({
        connection: connections[0],
        database: connections[0].database || '',
        schema: '',
      });
    }
    setObjectCategory(cat);
    setObjectsTabOpen(true);
    setActiveTabId(OBJECTS_TAB_ID);
  };

  const handleOpenTableData = (conn: ConnectionView, database: string, schema: string, table: string) => {
    setSelectedContext({ connection: conn, database, schema });
    setSelectedObject({ name: table, type: 'TABLE', schema });
    const id = `data:${conn.id}:${database}:${schema}:${table}`;
    openTab({
      id,
      kind: 'data',
      title: `${table}`,
      connectionId: conn.id,
      engine: conn.engine,
      database,
      schema,
      table,
    });
  };

  const handleDesignTable = (
    conn: ConnectionView,
    database: string,
    schema: string,
    table: string,
    mode: 'create' | 'alter',
  ) => {
    setSelectedContext({ connection: conn, database, schema });
    const id =
      mode === 'create'
        ? `design:create:${conn.id}:${database}:${schema}:${Date.now()}`
        : `design:alter:${conn.id}:${database}:${schema}:${table}`;
    openTab({
      id,
      kind: 'design',
      title: mode === 'create' ? `新建表 (${database || conn.name})` : `设计 · ${table}`,
      connectionId: conn.id,
      engine: conn.engine,
      database,
      schema,
      table,
      mode,
    });
  };

  const handleOpenQuery = (conn: ConnectionView, database: string, schema: string, initialSql?: string) => {
    setSelectedContext({ connection: conn, database, schema });
    const seq = tabs.filter((t) => t.kind === 'query').length + 1;
    const id = `query:${conn.id}:${Date.now()}`;
    openTab({
      id,
      kind: 'query',
      title: `查询 #${seq} · ${conn.name}`,
      connectionId: conn.id,
      engine: conn.engine,
      database,
      schema,
      initialSql,
    });
  };

  const resolveActiveTarget = (): { conn: ConnectionView; database: string; schema: string } | null => {
    if (connections.length === 0) return null;
    const active = tabs.find((t) => t.id === activeTabId);
    if (active) {
      const conn = connections.find((c) => c.id === active.connectionId) || connections[0];
      return { conn, database: active.database, schema: active.schema };
    }
    if (selectedContext) {
      const conn = connections.find((c) => c.id === selectedContext.connection.id) || connections[0];
      return {
        conn,
        database: selectedContext.database || conn.database || '',
        schema: selectedContext.schema,
      };
    }
    return { conn: connections[0], database: connections[0].database || '', schema: '' };
  };

  const handleQuickNewQuery = () => {
    const target = resolveActiveTarget();
    if (!target) {
      openNewConnectionModal();
      return;
    }
    handleOpenQuery(target.conn, target.database, target.schema);
  };

  const handleQuickNewTable = () => {
    const target = resolveActiveTarget();
    if (!target) {
      openNewConnectionModal();
      return;
    }
    const effectiveSch = target.schema || (target.conn.engine === 'sqlite' ? 'main' : '');
    handleDesignTable(target.conn, target.database, effectiveSch, '', 'create');
  };

  const handleDeleteConnection = async (conn: ConnectionView) => {
    if (!window.confirm(`确认删除连接「${conn.name}」吗？`)) return;
    await api.deleteConnection(conn.id);
    setConnections((prev) => prev.filter((c) => c.id !== conn.id));
    setTabs((prev) => prev.filter((t) => t.connectionId !== conn.id));
    if (selectedContext?.connection.id === conn.id) {
      setSelectedContext(null);
      setSelectedObject(null);
    }
  };

  const handleDisconnectConnection = async (conn: ConnectionView) => {
    await api.disconnectConnection(conn.id);
  };

  if (authLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-[#161618] text-xs font-medium text-[#9e9ea9]">
        正在加载 紫东直连...
      </div>
    );
  }

  if (authEnabled && !authenticated) {
    return <LoginModal onSuccess={(user) => void handleLoginSuccess(user)} />;
  }

  const activeTarget = resolveActiveTarget();
  const hasAnyOpenTab = objectsTabOpen || tabs.length > 0;

  return (
    <div className="flex h-screen flex-col bg-[#161618] text-[#f2f2f7] select-none">
      {/* Apple macOS Unified Vibrancy Toolbar */}
      <header className="flex h-16 shrink-0 items-center justify-between border-b border-white/[0.08] bg-[#212124]/95 px-4 backdrop-blur-xl">
        <div className="flex items-center gap-1.5">
          {/* App Brand */}
          <div className="mr-2.5 flex items-center gap-3 border-r border-white/[0.08] pr-4">
            <BrandLogo className="h-9 w-9 shrink-0 drop-shadow-sm" />
            <div>
              <div className="text-[15px] font-semibold tracking-tight text-white">紫东直连</div>
              <div className="text-[11.5px] font-normal text-[#9e9ea9]">多源数据库直连工作台</div>
            </div>
          </div>

          {/* 1. 连接 (Connection Dropdown) */}
          <div className="relative" onClick={(e) => e.stopPropagation()}>
            <button
              type="button"
              onClick={() => setConnMenuOpen((v) => !v)}
              className="apple-toolbar-btn"
              title="新建数据库连接"
            >
              <div className="flex items-center gap-0.5 text-[#52a9ff]">
                <Database className="h-5 w-5" strokeWidth={1.8} />
                <ChevronDown className="h-3.5 w-3.5 opacity-80" />
              </div>
              <span className="text-[12.5px] font-medium">连接</span>
            </button>

            {connMenuOpen && (
              <div className="absolute left-0 top-14 z-50 w-64 rounded-xl border border-white/[0.12] bg-[#2a2a2e]/95 p-1.5 text-[13.5px] shadow-2xl backdrop-blur-2xl">
                <div className="px-3 py-1.5 text-xs font-medium text-[#9e9ea9]">
                  新建数据库连接
                </div>
                {ENGINE_MENU_ITEMS.map((item) => (
                  <button
                    key={item.engine}
                    type="button"
                    onClick={() => {
                      setConnMenuOpen(false);
                      openNewConnectionModal(item.engine);
                    }}
                    className="flex w-full items-center justify-between rounded-[8px] px-3 py-2 text-left font-medium text-[#f2f2f7] transition-colors hover:bg-[#0a84ff] hover:text-white"
                  >
                    <span>{item.label}</span>
                    <span className="rounded bg-white/[0.08] px-1.5 py-0.5 font-mono text-xs text-[#cacad1]">
                      {item.badge}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* 2. 新建查询 (New Query) */}
          <button
            type="button"
            onClick={handleQuickNewQuery}
            className="apple-toolbar-btn"
            title="新建 SQL 查询窗口"
          >
            <Terminal className="h-5 w-5 text-[#30d158]" strokeWidth={1.8} />
            <span className="text-[12.5px] font-medium">新建查询</span>
          </button>

          <div className="mx-1.5 h-7 w-px bg-white/[0.08]" />

          {/* 3. 表总览 (Tables Overview - Only opens when explicitly clicked) */}
          <button
            type="button"
            onClick={() => openObjectsOverview('tables')}
            className={`apple-toolbar-btn ${
              activeTabId === OBJECTS_TAB_ID && objectCategory === 'tables' ? 'apple-toolbar-btn-active' : ''
            }`}
            title="打开当前数据库表对象列表"
          >
            <Table2 className="h-5 w-5 text-[#64d2ff]" strokeWidth={1.8} />
            <span className="text-[12.5px] font-medium">表总览</span>
          </button>

          {/* 4. 视图总览 (Views Overview) */}
          <button
            type="button"
            onClick={() => openObjectsOverview('views')}
            className={`apple-toolbar-btn ${
              activeTabId === OBJECTS_TAB_ID && objectCategory === 'views' ? 'apple-toolbar-btn-active' : ''
            }`}
            title="打开当前数据库视图列表"
          >
            <Eye className="h-5 w-5 text-[#5ac8fa]" strokeWidth={1.8} />
            <span className="text-[12.5px] font-medium">视图</span>
          </button>

          {/* 5. 查询历史 (Recent Queries) */}
          <button
            type="button"
            onClick={() => openObjectsOverview('queries')}
            className={`apple-toolbar-btn ${
              activeTabId === OBJECTS_TAB_ID && objectCategory === 'queries' ? 'apple-toolbar-btn-active' : ''
            }`}
            title="查看最近执行的 SQL 查询"
          >
            <Layers className="h-5 w-5 text-[#ff9f0a]" strokeWidth={1.8} />
            <span className="text-[12.5px] font-medium">查询历史</span>
          </button>

          <div className="mx-1.5 h-7 w-px bg-white/[0.08]" />

          {/* 6. 新建表 (New Table Designer) */}
          <button
            type="button"
            onClick={handleQuickNewTable}
            className="apple-toolbar-btn"
            title="打开表结构设计器创建新表"
          >
            <FilePlus2 className="h-5 w-5 text-[#ffd60a]" strokeWidth={1.8} />
            <span className="text-[12.5px] font-medium">新建表</span>
          </button>

          {/* 7. 导入向导 (Import Wizard) */}
          <button
            type="button"
            onClick={() => {
              if (!activeTarget) {
                openNewConnectionModal();
                return;
              }
              setGlobalIoModal('import');
            }}
            className="apple-toolbar-btn"
            title="从 CSV / JSON / SQL 脚本导入数据"
          >
            <Upload className="h-5 w-5 text-[#5e5ce6]" strokeWidth={1.8} />
            <span className="text-[12.5px] font-medium">导入</span>
          </button>

          {/* 8. 导出向导 (Export Wizard) */}
          <button
            type="button"
            onClick={() => {
              if (!activeTarget) {
                openNewConnectionModal();
                return;
              }
              setGlobalIoModal('export');
            }}
            className="apple-toolbar-btn"
            title="导出表或查询结果为 CSV / JSON / SQL"
          >
            <Download className="h-5 w-5 text-[#30d158]" strokeWidth={1.8} />
            <span className="text-[12.5px] font-medium">导出</span>
          </button>
        </div>

        {/* Right Side Controls: Info Pane Toggle + User */}
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setShowRightPane((v) => !v)}
            className={showRightPane ? 'apple-btn-primary' : 'apple-btn-secondary'}
            title="显示/隐藏右侧属性与 DDL 检查器"
          >
            <PanelRight className="h-4 w-4" />
            <span className="hidden sm:inline">属性与 DDL</span>
          </button>

          {authEnabled && (
            <div className="flex items-center gap-2 border-l border-white/[0.08] pl-2.5 text-xs text-[#9e9ea9]">
              <span className="hidden font-medium text-[#e4e4e9] md:inline">{currentUser}</span>
              <button
                type="button"
                onClick={() => void handleLogout()}
                className="apple-btn-secondary"
              >
                <LogOut className="h-3.5 w-3.5" />
                退出
              </button>
            </div>
          )}
        </div>
      </header>

      {/* Main Split Area */}
      <div className="flex min-h-0 flex-1">
        <SidebarTree
          connections={connections}
          capabilities={capabilities}
          selectedContext={selectedContext}
          selectedObject={selectedObject}
          onSelectContext={(ctx) => {
            setSelectedContext(ctx);
          }}
          onSelectObject={(obj) => {
            setSelectedObject(obj);
          }}
          onOpenObjectsOverview={(cat, ctx) => {
            openObjectsOverview(cat, ctx);
          }}
          onNewConnection={(eng) => openNewConnectionModal(eng)}
          onEditConnection={(c) => {
            setEditingConn(c);
            setConnModalOpen(true);
          }}
          onDeleteConnection={(c) => void handleDeleteConnection(c)}
          onDisconnectConnection={(c) => void handleDisconnectConnection(c)}
          onOpenTableData={handleOpenTableData}
          onDesignTable={handleDesignTable}
          onOpenQuery={handleOpenQuery}
          refreshSignal={refreshSignal}
        />

        {/* Center Multi-Tab Workspace */}
        <main className="flex min-w-0 flex-1 flex-col bg-[#161618]">
          {/* macOS Safari/Xcode Style Tab Bar (Only shown when at least 1 tab is open) */}
          {hasAnyOpenTab && (
            <div className="flex items-center gap-1.5 overflow-x-auto border-b border-white/[0.08] bg-[#1e1e21] px-3 py-2">
              {objectsTabOpen && (
                <div
                  onClick={() => setActiveTabId(OBJECTS_TAB_ID)}
                  className={`flex cursor-pointer items-center gap-2 rounded-[8px] px-3.5 py-1.5 text-[13.5px] font-medium transition-all ${
                    activeTabId === OBJECTS_TAB_ID
                      ? 'bg-white/[0.14] text-white shadow-sm'
                      : 'text-[#9e9ea9] hover:bg-white/[0.06] hover:text-[#e4e4e9]'
                  }`}
                >
                  <Layers className="h-4 w-4 shrink-0 text-[#52a9ff]" />
                  <span>对象总览</span>
                  {selectedContext?.database && (
                    <span className="max-w-[120px] truncate text-xs font-normal text-[#9e9ea9]">
                      · {selectedContext.database}
                    </span>
                  )}
                  <button
                    type="button"
                    onClick={(e) => closeTab(OBJECTS_TAB_ID, e)}
                    className="ml-1 rounded-full p-0.5 text-[#9e9ea9] hover:bg-white/[0.15] hover:text-white"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              )}

              {tabs.map((tab) => {
                const isActive = tab.id === activeTabId;
                return (
                  <div
                    key={tab.id}
                    onClick={() => setActiveTabId(tab.id)}
                    className={`flex max-w-[260px] cursor-pointer items-center gap-2 rounded-[8px] px-3.5 py-1.5 text-[13.5px] font-medium transition-all ${
                      isActive
                        ? 'bg-white/[0.14] text-white shadow-sm'
                        : 'text-[#9e9ea9] hover:bg-white/[0.06] hover:text-[#e4e4e9]'
                    }`}
                  >
                    {tab.kind === 'data' && <Table2 className="h-4 w-4 shrink-0 text-[#64d2ff]" />}
                    {tab.kind === 'query' && <Terminal className="h-4 w-4 shrink-0 text-[#30d158]" />}
                    {tab.kind === 'design' && <Wrench className="h-4 w-4 shrink-0 text-[#ff9f0a]" />}
                    <span className="truncate">{tab.title}</span>
                    <button
                      type="button"
                      onClick={(e) => closeTab(tab.id, e)}
                      className="ml-1 rounded-full p-0.5 text-[#9e9ea9] hover:bg-white/[0.15] hover:text-white"
                    >
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </div>
                );
              })}
            </div>
          )}

          {/* Tab Content or Clean Apple Empty State */}
          <div className="min-h-0 flex-1">
            {!hasAnyOpenTab ? (
              <div className="flex h-full flex-col items-center justify-center gap-6 p-8 text-center">
                <BrandLogo className="h-20 w-20 drop-shadow-xl" />
                <div className="space-y-2">
                  <h2 className="text-[18px] font-semibold tracking-tight text-white">
                    {connections.length > 0 ? '从左侧导航树选择数据表开始操作' : '欢迎使用 紫东直连'}
                  </h2>
                  <p className="max-w-lg text-[14px] leading-relaxed text-[#9e9ea9]">
                    {connections.length > 0
                      ? '在左侧展开连接与数据库后，点击任意表即可直接打开数据视图；也可随时新建 SQL 查询或设计新表。'
                      : '支持 MySQL、PostgreSQL、SQLite、SQL Server、Oracle 与达梦 DM8，内置 SSH 隧道与可视化表结构设计器。'}
                  </p>
                </div>
                <div className="flex flex-wrap items-center justify-center gap-3">
                  {connections.length > 0 && (
                    <button
                      type="button"
                      onClick={handleQuickNewQuery}
                      className="apple-btn-primary px-5 py-2 text-[14px]"
                    >
                      <Terminal className="h-4 w-4" />
                      新建 SQL 查询
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => openNewConnectionModal()}
                    className={
                      connections.length > 0
                        ? 'apple-btn-secondary px-5 py-2 text-[14px]'
                        : 'apple-btn-primary px-5 py-2 text-[14px]'
                    }
                  >
                    <Plus className="h-4 w-4" />
                    新建数据库连接
                  </button>
                </div>
              </div>
            ) : (
              <>
                {objectsTabOpen && (
                  <div className={activeTabId === OBJECTS_TAB_ID ? 'h-full' : 'hidden'}>
                    <ObjectBrowserPane
                      connections={connections}
                      capabilities={capabilities}
                      connection={selectedContext?.connection || connections[0] || null}
                      database={selectedContext?.database || ''}
                      schema={selectedContext?.schema || ''}
                      category={objectCategory}
                      onCategoryChange={setObjectCategory}
                      selectedObject={selectedObject}
                      onSelectObject={setSelectedObject}
                      onSelectDatabaseSchema={(conn, dbName, schName) => {
                        setSelectedContext({ connection: conn, database: dbName, schema: schName });
                      }}
                      onOpenTableData={handleOpenTableData}
                      onDesignTable={handleDesignTable}
                      onOpenQuery={handleOpenQuery}
                      onNewConnection={(eng) => openNewConnectionModal(eng)}
                      refreshSignal={refreshSignal}
                      onObjectsChanged={() => setRefreshSignal((n) => n + 1)}
                    />
                  </div>
                )}

                {tabs.map((tab) => {
                  const hidden = tab.id !== activeTabId;
                  return (
                    <div key={tab.id} className={hidden ? 'hidden' : 'h-full'}>
                      {tab.kind === 'data' && (
                        <DataGridTab
                          connectionId={tab.connectionId}
                          engine={tab.engine}
                          database={tab.database}
                          schema={tab.schema}
                          table={tab.table}
                          onOpenDesign={() => {
                            const conn = connections.find((c) => c.id === tab.connectionId);
                            if (conn) {
                              handleDesignTable(conn, tab.database, tab.schema, tab.table, 'alter');
                            }
                          }}
                          onOpenSql={(sqlText) => {
                            const conn = connections.find((c) => c.id === tab.connectionId);
                            if (conn) {
                              handleOpenQuery(conn, tab.database, tab.schema, sqlText);
                            }
                          }}
                        />
                      )}
                      {tab.kind === 'query' && (
                        <SqlWorkspace
                          connections={connections}
                          capabilities={capabilities}
                          connectionId={tab.connectionId}
                          engine={tab.engine}
                          database={tab.database}
                          schema={tab.schema}
                          initialSql={tab.initialSql}
                          onContextChange={(conn, dbName, schName) => {
                            setTabs((prev) =>
                              prev.map((t) =>
                                t.id === tab.id && t.kind === 'query'
                                  ? {
                                      ...t,
                                      connectionId: conn.id,
                                      engine: conn.engine,
                                      database: dbName,
                                      schema: schName,
                                      title: `查询 · ${conn.name}`,
                                    }
                                  : t,
                              ),
                            );
                          }}
                        />
                      )}
                      {tab.kind === 'design' && (
                        <StructureDesigner
                          connectionId={tab.connectionId}
                          engine={tab.engine}
                          capabilities={capabilities[tab.engine]}
                          database={tab.database}
                          schema={tab.schema}
                          table={tab.table}
                          mode={tab.mode}
                          onApplied={(finalTable) => {
                            setRefreshSignal((n) => n + 1);
                            setTabs((prev) =>
                              prev.map((t) =>
                                t.id === tab.id && t.kind === 'design'
                                  ? { ...t, table: finalTable, mode: 'alter', title: `设计 · ${finalTable}` }
                                  : t,
                              ),
                            );
                          }}
                          onOpenData={(tblName) => {
                            const conn = connections.find((c) => c.id === tab.connectionId);
                            if (conn) {
                              handleOpenTableData(conn, tab.database, tab.schema, tblName);
                            }
                          }}
                        />
                      )}
                    </div>
                  );
                })}
              </>
            )}
          </div>
        </main>

        {/* Right Collapsible Property & DDL Inspector */}
        {showRightPane && (
          <RightInfoPane
            connection={selectedContext?.connection || connections[0] || null}
            database={selectedContext?.database || ''}
            schema={selectedContext?.schema || ''}
            selectedObject={selectedObject}
            onOpenTableData={handleOpenTableData}
            onDesignTable={handleDesignTable}
            onOpenQuery={handleOpenQuery}
          />
        )}
      </div>

      {/* Bottom macOS Status Bar */}
      <footer className="flex h-8 shrink-0 items-center justify-between border-t border-white/[0.08] bg-[#1e1e21] px-4 text-[12.5px] text-[#9e9ea9]">
        <div className="flex items-center gap-2.5 truncate">
          {selectedContext?.connection ? (
            <>
              <span className="flex items-center gap-1.5 font-medium text-[#e4e4e9]">
                <span className="h-2 w-2 rounded-full bg-[#30d158]" />
                <span>{selectedContext.connection.name}</span>
                <span className="text-[#767682]">({selectedContext.connection.engine.toUpperCase()})</span>
              </span>
              {selectedContext.database && (
                <>
                  <span className="text-white/[0.15]">/</span>
                  <span>{selectedContext.database}</span>
                </>
              )}
              {selectedContext.schema && (
                <>
                  <span className="text-white/[0.15]">/</span>
                  <span>{selectedContext.schema}</span>
                </>
              )}
              {selectedObject && (
                <>
                  <span className="text-white/[0.15]">/</span>
                  <span className="font-mono text-[#52a9ff]">{selectedObject.name}</span>
                </>
              )}
              {selectedContext.connection.ssh.enabled && (
                <>
                  <span className="text-white/[0.15]">·</span>
                  <span className="inline-flex items-center gap-1 text-[#ff9f0a]">
                    <KeyRound className="h-3 w-3" />
                    SSH ({selectedContext.connection.ssh.host}:{selectedContext.connection.ssh.port})
                  </span>
                </>
              )}
            </>
          ) : (
            <span>就绪 · 请在左侧展开数据库连接</span>
          )}
        </div>
        <div className="hidden items-center gap-2 text-[#767682] sm:flex">
          <span>MySQL · PostgreSQL · SQLite · SQL Server · Oracle · 达梦 DM8</span>
        </div>
      </footer>

      {connModalOpen && (
        <ConnectionModal
          editing={editingConn}
          initialEngine={initialModalEngine}
          onClose={() => setConnModalOpen(false)}
          onSaved={(saved) => {
            setConnections((prev) => {
              const idx = prev.findIndex((c) => c.id === saved.id);
              if (idx >= 0) {
                return prev.map((c) => (c.id === saved.id ? saved : c));
              }
              return [...prev, saved];
            });
            setConnModalOpen(false);
          }}
        />
      )}

      {globalIoModal && activeTarget && (
        <ImportExportModal
          mode={globalIoModal}
          connectionId={activeTarget.conn.id}
          database={activeTarget.database}
          schema={activeTarget.schema || (activeTarget.conn.engine === 'sqlite' ? 'main' : '')}
          table={selectedObject?.name || ''}
          onClose={() => setGlobalIoModal(null)}
          onImported={() => setRefreshSignal((n) => n + 1)}
        />
      )}
    </div>
  );
}
