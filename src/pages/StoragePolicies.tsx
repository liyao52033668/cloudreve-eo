import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Layout,
  Card,
  Button,
  Space,
  message,
  Modal,
  Form,
  Input,
  InputNumber,
  Switch,
  Tag,
  Empty,
  Popconfirm,
  Typography,
  Select,
  Table,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  PlusOutlined,
  ReloadOutlined,
  EditOutlined,
  DeleteOutlined,
  StarOutlined,
  StarFilled,
  CloudServerOutlined,
  GlobalOutlined,
  SearchOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import {
  listAdminPolicies,
  getAdminPolicy,
  createPolicy,
  updatePolicy,
  deletePolicy,
  setDefaultPolicy,
  setPolicyCORS,
  type StoragePolicyAdmin,
  type PolicyForm,
} from '../api/policies'
import { getProfile } from '../api/user'
import AppHeader from '../components/AppHeader'
import TeraBoxAuth from '../components/TeraBoxAuth'
import BaiduAuth from '../components/BaiduAuth'

const { Content } = Layout
const { Paragraph } = Typography

const GiB = 1024 * 1024 * 1024
const MiB = 1024 * 1024

const emptyForm: PolicyForm = {
  name: '',
  type: 's3',
  endpoint: '',
  region: 'us-east-1',
  bucket: '',
  access_key: '',
  secret_key: '',
  force_path_style: true,
  custom_host: '',
  base_path: '',
  branch: '',
  chunk_size: 0,
  is_default: false,
  default_quota: 0,
}

function formatBytes(n: number): string {
  if (!n || n <= 0) return '0（未配置）'
  if (n >= GiB) {
    const g = n / GiB
    return Number.isInteger(g) ? `${g} GiB` : `${g.toFixed(2)} GiB`
  }
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(2)} MiB`
  if (n >= 1024) return `${(n / 1024).toFixed(2)} KiB`
  return `${n} B`
}

/** 非 S3/GitHub 存储类型的服务商官网，在存储类型选择下方显示。 */
const officialSites: Record<string, { label: string; url: string }> = {
  terabox: { label: 'TeraBox 开放平台', url: 'https://www.terabox.com' },
  baidu: { label: '百度网盘开放平台', url: 'https://pan.baidu.com/union/console/app' },
  filen: { label: 'Filen', url: 'https://filen.io/r/2b8a482d566c14ebef8f7c634d9a42ea' },
  dropbox: { label: 'Dropbox Developers', url: 'https://www.dropbox.com/referrals/AAAPux-KYGhTqYV8Jfes9AZXxj53M4oeAcA?src=global9' },
}

export default function StoragePolicies() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [policies, setPolicies] = useState<StoragePolicyAdmin[]>([])
  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm<PolicyForm & { default_quota_gib?: number | null; chunk_size_mib?: number | null }>()
  const [policyType, setPolicyType] = useState<'s3' | 'github' | 'terabox' | 'filen' | 'dropbox' | 'baidu'>('s3')
  const [filterType, setFilterType] = useState<string | undefined>(undefined)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [authTarget, setAuthTarget] = useState<{ id: number; type: string } | null>(null)

  const typeLabel = (t: string) =>
    t === 'github'
      ? 'GitHub'
      : t === 'terabox'
        ? 'TeraBox'
        : t === 'baidu'
          ? '百度网盘'
          : t === 'filen'
            ? 'Filen'
            : t === 'dropbox'
              ? 'Dropbox'
              : 'S3 兼容'

  // 分类选项只包含已添加的存储类型
  const categoryOptions = useMemo(() => {
    const present = Array.from(new Set(policies.map((p) => p.type || 's3')))
    const order = (t: string) => (t === 'github' ? 1 : t === 'terabox' ? 2 : t === 'baidu' ? 3 : t === 'filen' ? 4 : t === 'dropbox' ? 5 : 0)
    return present.sort((a, b) => order(a) - order(b)).map((t) => ({ label: typeLabel(t), value: t }))
  }, [policies])

  useEffect(() => {
    if (filterType && !categoryOptions.some((o) => o.value === filterType)) {
      setFilterType(undefined)
    }
  }, [categoryOptions, filterType])

  const filteredPolicies = useMemo(() => {
    const kw = searchKeyword.trim().toLowerCase()
    return policies.filter((p) => {
      if (filterType && (p.type || 's3') !== filterType) return false
      if (!kw) return true
      return (
        p.name.toLowerCase().includes(kw) ||
        (p.endpoint || '').toLowerCase().includes(kw)
      )
    })
  }, [policies, filterType, searchKeyword])

  const ensureAdmin = useCallback(async () => {
    try {
      const res = await getProfile()
      if (!res.data.user?.is_admin) {
        message.error('需要管理员权限')
        navigate('/')
        return false
      }
      return true
    } catch {
      navigate('/login')
      return false
    }
  }, [navigate])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const ok = await ensureAdmin()
      if (!ok) return
      const res = await listAdminPolicies()
      setPolicies(res.data.policies || [])
    } catch (err: any) {
      if (err.response?.status === 403) {
        message.error('需要管理员权限')
        navigate('/')
      } else {
        message.error(err.response?.data?.error || '加载失败')
      }
    } finally {
      setLoading(false)
    }
  }, [ensureAdmin, navigate])

  useEffect(() => {
    load()
  }, [load])

  const openCreate = () => {
    setEditingId(null)
    setPolicyType('s3')
    form.setFieldsValue({
      ...emptyForm,
      region: '',
      is_default: policies.length === 0,
      default_quota_gib: 0,
      chunk_size_mib: 0,
    })
    setModalOpen(true)
  }

  const openEdit = async (id: number) => {
    try {
      const res = await getAdminPolicy(id)
      const p = res.data.policy
      setEditingId(id)
      setPolicyType(p.type || 's3')
      form.setFieldsValue({
        name: p.name,
        type: p.type || 's3',
        endpoint: p.endpoint,
        // terabox 的 region 复用为 Private Secret，filen/dropbox/baidu 不使用 region；编辑时留空表示不修改
        region: p.type === 'terabox' || p.type === 'filen' || p.type === 'dropbox' || p.type === 'baidu' ? '' : p.region || 'us-east-1',
        bucket: p.bucket,
        access_key: p.access_key,
        secret_key: '', // 留空表示不修改
        force_path_style: p.force_path_style !== false,
        custom_host: p.custom_host || '',
        base_path: p.base_path || '',
        branch: p.branch || '',
        is_default: p.is_default,
        default_quota_gib: (p.default_quota || 0) / GiB,
        chunk_size_mib: (p.chunk_size || 0) / MiB,
      })
      setModalOpen(true)
    } catch (err: any) {
      message.error(err.response?.data?.error || '加载策略失败')
    }
  }

  const handleSave = async () => {
    try {
      const values = await form.validateFields()
      setSaving(true)
      const gib = Number(values.default_quota_gib ?? 0)
      if (Number.isNaN(gib) || gib < 0) {
        message.error('默认配额不能为负数')
        return
      }
      const chunkMib = Number(values.chunk_size_mib ?? 0)
      if (Number.isNaN(chunkMib) || chunkMib < 0) {
        message.error('分片大小不能为负数')
        return
      }
      if (values.type !== 'terabox' && values.type !== 'filen' && values.type !== 'dropbox' && values.type !== 'baidu' && chunkMib !== 0 && chunkMib < 5) {
        message.error('分片大小非 0 时至少为 5 MiB（S3 协议要求）')
        return
      }
      const payload: PolicyForm = {
        name: values.name,
        type: values.type || 's3',
        endpoint: values.endpoint,
        region: (values.region || '').trim(),
        bucket: values.bucket,
        access_key: values.access_key,
        secret_key: values.secret_key || '',
        force_path_style: values.force_path_style !== false,
        custom_host: (values.custom_host || '').trim().replace(/\/+$/, ''),
        base_path: (values.base_path || '').trim().replace(/^\/+|\/+$/g, ''),
        branch: (values.branch || '').trim(),
        chunk_size: Math.round(chunkMib * MiB),
        is_default: !!values.is_default,
        default_quota: Math.round(gib * GiB),
      }
      if (editingId == null) {
        if (!payload.secret_key) {
          message.error(
            payload.type === 'terabox'
              ? '新建时 Client Secret 不能为空'
              : payload.type === 'baidu'
                ? '新建时 SecretKey 不能为空'
                : '新建时 Secret Key 不能为空',
          )
          return
        }
        await createPolicy(payload)
        message.success('已添加存储策略')
      } else {
        await updatePolicy(editingId, payload)
        message.success('已更新存储策略')
      }
      setModalOpen(false)
      load()
    } catch (err: any) {
      if (err?.errorFields) return // form validation
      message.error(err.response?.data?.error || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (id: number) => {
    try {
      await deletePolicy(id)
      message.success('已删除')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '删除失败')
    }
  }

  const handleSetDefault = async (id: number) => {
    try {
      await setDefaultPolicy(id)
      message.success('已设为默认策略')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || '设置失败')
    }
  }

  const handleSetCORS = async (id: number) => {
    try {
      await setPolicyCORS(id)
      message.success('存储桶 CORS 已配置，浏览器可直传')
      load()
    } catch (err: any) {
      message.error(err.response?.data?.error || 'CORS 配置失败，请检查密钥权限或到服务商控制台手动配置')
    }
  }

  const columns: TableColumnsType<StoragePolicyAdmin> = [
    {
      title: '策略名称',
      dataIndex: 'name',
      width: 160,
      render: (_, p) => (
        <Space>
          <CloudServerOutlined style={{ color: '#1677ff' }} />
          <span>{p.name}</span>
          {p.is_default && <Tag color="blue">默认</Tag>}
        </Space>
      ),
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 170,
      render: (t: string, p) => (
        <Space size={4}>
          <Tag>{typeLabel(t || 's3')}</Tag>
          {p.cors_enabled && <Tag color="green">CORS</Tag>}
          {(t === 'terabox' || t === 'baidu') &&
            (p.authorized ? <Tag color="green">已授权</Tag> : <Tag color="orange">未授权</Tag>)}
        </Space>
      ),
    },
    {
      title: '存储位置',
      width: 220,
      render: (_, p) => {
        // filen 无 endpoint，展示账号邮箱；其余展示自定义域名/Endpoint
        const location = p.type === 'filen' ? p.access_key : p.custom_host || p.endpoint
        return <span style={{ wordBreak: 'break-all' }}>{location || '—'}</span>
      },
    },
    {
      title: '上传路径',
      dataIndex: 'base_path',
      width: 140,
      render: (v: string, p) =>
        v
          ? `/${v}/`
          : p.type === 'github'
            ? '（仓库根）'
            : p.type === 'filen'
              ? '（Filen 根）'
              : p.type === 'dropbox'
                ? '（Dropbox 根）'
                : p.type === 'baidu'
                  ? '（网盘根目录）'
                  : '（bucket 根）',
    },
    {
      title: '每用户配额',
      dataIndex: 'default_quota',
      width: 140,
      render: (v: number) => formatBytes(v || 0),
    },
    {
      title: '分片大小',
      dataIndex: 'chunk_size',
      width: 130,
      render: (v: number, p) =>
        p.type === 'github' ? '—' : v > 0 ? `${(v / MiB).toFixed(0)} MiB` : '默认（25 MiB）',
    },
    {
      title: '操作',
      width: 300,
      render: (_, p) => (
        <Space>
          <Button
            type="link"
            size="small"
            icon={p.is_default ? <StarFilled /> : <StarOutlined />}
            disabled={p.is_default}
            onClick={() => handleSetDefault(p.id)}
          >
            {p.is_default ? '默认' : '设为默认'}
          </Button>
          <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(p.id)}>
            编辑
          </Button>
          {(p.type === 'terabox' || p.type === 'baidu') && (
            <Button
              type="link"
              size="small"
              icon={<SafetyCertificateOutlined />}
              onClick={() => setAuthTarget({ id: p.id, type: p.type })}
            >
              {p.authorized ? '重新授权' : '授权'}
            </Button>
          )}
          {p.type === 's3' && (
            <Popconfirm
              title="配置存储桶 CORS？"
              description="将向该 Bucket 写入允许浏览器直传（含分片上传 ETag 暴露）的 CORS 规则。"
              onConfirm={() => handleSetCORS(p.id)}
            >
              <Button type="link" size="small" icon={<GlobalOutlined />}>
                CORS
              </Button>
            </Popconfirm>
          )}
          <Popconfirm
            title="确认删除该策略？"
            description="已上传到该策略的文件记录不会自动迁移。"
            onConfirm={() => handleDelete(p.id)}
          >
            <Button type="link" size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <AppHeader title="存储策略" />
      <Content style={{ padding: 24, maxWidth: 1308, margin: '0 auto', width: '100%' }}>
        <Space style={{ marginBottom: 16, flexWrap: 'wrap' }}>
          <Input
            allowClear
            placeholder="搜索策略名称 / Endpoint"
            prefix={<SearchOutlined />}
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            style={{ width: 220 }}
          />
          <Select
            allowClear
            placeholder="分类"
            options={categoryOptions}
            value={filterType}
            onChange={(v) => setFilterType(v)}
            style={{ width: 160 }}
            disabled={categoryOptions.length === 0}
          />
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            添加存储策略
          </Button>
        </Space>

        <Paragraph type="secondary" style={{ marginBottom: 16 }}>
          在此添加多套互相独立的存储（S3 兼容：腾讯云 COS、阿里云 OSS、MinIO、Cloudflare R2；GitHub；TeraBox
          开放平台；百度网盘开放平台；Filen 端到端加密网盘；Dropbox）。每套使用各自凭证与用户默认配额；上传时可任选其一。配置保存在数据库，修改后立即生效，无需环境变量与重启。
          TeraBox 类型创建后需在列表中点击「授权」完成 OAuth 扫码/网页授权方可使用；百度网盘填写开放平台 AppKey 与
          SecretKey，创建后需在列表中点击「授权」完成 OAuth 授权方可使用；Filen 填写账号邮箱与密码即可；Dropbox 填写
          App Console 生成的 Access Token 即可。
        </Paragraph>

        {policies.length === 0 && !loading ? (
          <Card>
            <Empty description="尚未配置存储策略">
              <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                添加第一个策略
              </Button>
            </Empty>
          </Card>
        ) : (
          <Table
            rowKey="id"
            columns={columns}
            dataSource={filteredPolicies}
            loading={loading}
            pagination={{ defaultPageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 个策略` }}
            locale={{
              emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的存储策略" />,
            }}
            scroll={{ x: 1260 }}
          />
        )}
      </Content>

      <Modal
        title={editingId == null ? '添加存储策略' : '编辑存储策略'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={handleSave}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        width={560}
        destroyOnClose
      >
        <Form
          form={form}
          layout="vertical"
          initialValues={{ ...emptyForm, default_quota_gib: 0, chunk_size_mib: 0 }}
          onValuesChange={(changedValues) => {
            if (changedValues.type) {
              setPolicyType(changedValues.type)
            }
          }}
        >
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: '请输入策略名称' }]}
            extra="展示名，也用于写入文件的 storage_policy 字段"
          >
            <Input placeholder="例如 oss、minio、cos" disabled={editingId != null} />
          </Form.Item>

          <Form.Item
            name="type"
            label="存储类型"
            rules={[{ required: true, message: '请选择存储类型' }]}
            extra={
              officialSites[policyType] ? (
                <span>
                  选择存储服务提供商 · 官网：{' '}
                  <Typography.Link
                    href={officialSites[policyType].url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {officialSites[policyType].label}
                  </Typography.Link>
                </span>
              ) : (
                '选择存储服务提供商'
              )
            }
          >
            <Select disabled={editingId != null}>
              <Select.Option value="s3">S3 兼容存储</Select.Option>
              <Select.Option value="github">GitHub</Select.Option>
              <Select.Option value="terabox">TeraBox</Select.Option>
              <Select.Option value="baidu">百度网盘</Select.Option>
              <Select.Option value="filen">Filen</Select.Option>
              <Select.Option value="dropbox">Dropbox</Select.Option>
            </Select>
          </Form.Item>

          {policyType === 'terabox' && (
            <>
              <Form.Item
                name="access_key"
                label="Client ID"
                rules={[{ required: true, message: '请输入 Client ID' }]}
                extra="向 TeraBox 开放平台申请的 AppKey"
              >
                <Input placeholder="client_id" autoComplete="off" />
              </Form.Item>
              <Form.Item
                name="region"
                label="Private Secret"
                rules={editingId == null ? [{ required: true, message: '请输入 Private Secret' }] : []}
                extra="用于生成请求签名（sign）的私钥；编辑时留空表示不修改"
              >
                <Input.Password placeholder={editingId != null ? '留空则不修改' : 'private_secret'} autoComplete="new-password" />
              </Form.Item>
              <Form.Item
                name="endpoint"
                label="应用根目录"
                rules={[{ required: true, message: '请输入应用根目录' }]}
                extra='TeraBox 分配的应用文件空间，形如 /From: Other Applications/应用名-应用ID/'
              >
                <Input placeholder="/From: Other Applications/MyApp-123/" />
              </Form.Item>
            </>
          )}

          {policyType === 'baidu' && (
            <>
              <Form.Item
                name="access_key"
                label="AppKey"
                rules={[{ required: true, message: '请输入 AppKey' }]}
                extra="百度网盘开放平台应用的 AppKey"
              >
                <Input placeholder="AppKey" autoComplete="off" />
              </Form.Item>
              <Form.Item
                name="endpoint"
                label="回调地址"
                extra={`OAuth 回调地址（redirect_uri），须与开放平台应用配置一致；本站可填 ${window.location.origin}/api/oauth/baidu/callback（授权后自动完成）。留空使用 oob 模式（授权后手动粘贴授权码）`}
              >
                <Input placeholder="https://example.com/callback 或留空" />
              </Form.Item>
              <Form.Item
                name="base_path"
                label="存储路径前缀"
                extra="文件存储在网盘的该目录下，留空默认 /apps/cloudreve-eo"
              >
                <Input placeholder="apps/cloudreve-eo" allowClear />
              </Form.Item>
            </>
          )}

          {policyType === 'filen' && (
            <>
              <Form.Item
                name="access_key"
                label="Filen 邮箱"
                rules={[{ required: true, message: '请输入 Filen 账号邮箱' }]}
                extra="用于登录 Filen 的账号邮箱"
              >
                <Input placeholder="you@example.com" autoComplete="off" />
              </Form.Item>
              <Form.Item
                name="base_path"
                label="存储路径前缀"
                extra="文件将存储在该目录下（相对 Filen 根目录），留空默认 cloudreve-eo"
              >
                <Input placeholder="cloudreve-eo" allowClear />
              </Form.Item>
            </>
          )}

          {policyType === 'dropbox' && (
            <Form.Item
              name="base_path"
              label="存储路径前缀"
              extra="文件将存储在该目录下（相对 Dropbox 根目录），留空表示 Dropbox 根目录"
            >
              <Input placeholder="例如 cloudreve-eo" allowClear />
            </Form.Item>
          )}

          {policyType === 's3' && (
            <>
              <Form.Item
                name="bucket"
                label="Bucket 名称"
                rules={[{ required: true, message: '请输入 Bucket' }]}
              >
                <Input placeholder="your-bucket" />
              </Form.Item>
              <Form.Item
                name="endpoint"
                label="Endpoint"
                rules={[{ required: true, message: '请输入 Endpoint' }]}
                extra="S3 API 端点，如 https://oss-cn-shanghai.aliyuncs.com 或 https://cos.ap-guangzhou.myqcloud.com"
              >
                <Input placeholder="https://..." />
              </Form.Item>
              <Form.Item name="region" label="Region" extra="部分服务商必填，默认 us-east-1">
                <Input placeholder="us-east-1" />
              </Form.Item>
              <Form.Item
                name="base_path"
                label="存储路径前缀"
                extra="文件将存储在 bucket 的这个目录下，留空表示 bucket 根目录"
              >
                <Input placeholder="例如 files 或 uploads" allowClear />
              </Form.Item>
              <Form.Item
                name="force_path_style"
                label="强制 ForcePathStyle"
                valuePropName="checked"
                extra="MinIO 与多数私有/兼容 S3 需开启；AWS 官方 S3 可关闭（virtual-hosted）。"
              >
                <Switch checkedChildren="开" unCheckedChildren="关" />
              </Form.Item>
            </>
          )}

          {policyType === 'github' && (
            <>
              <Form.Item
                name="endpoint"
                label="仓库地址"
                rules={[{ required: true, message: '请输入 GitHub 仓库地址' }]}
                extra="格式：owner/repo 或 https://github.com/owner/repo"
              >
                <Input placeholder="例如 myuser/myrepo" />
              </Form.Item>
              <Form.Item
                name="branch"
                label="分支"
                extra="文件将提交到这个分支，留空默认使用 main 分支"
              >
                <Input placeholder="例如 main 或 master" allowClear />
              </Form.Item>
              <Form.Item
                name="base_path"
                label="存储路径前缀"
                extra="文件将存储在仓库的这个目录下，留空表示仓库根目录"
              >
                <Input placeholder="例如 files 或 cloudreve/uploads" allowClear />
              </Form.Item>
            </>
          )}

          <Form.Item
            name="custom_host"
            label="自定义域名"
            extra="COS / 七牛等的 CDN 加速域名，如 https://cdn.example.com。配置后下载与预览链接改用该域名；留空则用 Endpoint。部分服务商需在 CDN 侧将回源 Host 设为 Bucket 域名。"
          >
            <Input placeholder="例如 https://cdn.example.com" allowClear />
          </Form.Item>

          {policyType === 's3' && (
            <Form.Item
              name="access_key"
              label="Access Key"
              rules={[{ required: true, message: '请输入 Access Key' }]}
            >
              <Input placeholder="Access Key ID" autoComplete="off" />
            </Form.Item>
          )}

          <Form.Item
            name="secret_key"
            label={
              policyType === 'github'
                ? 'GitHub Token'
                : policyType === 'terabox'
                  ? 'Client Secret'
                  : policyType === 'baidu'
                    ? 'SecretKey'
                    : policyType === 'filen'
                      ? 'Filen 密码'
                      : policyType === 'dropbox'
                        ? 'Access Token'
                        : 'Secret Key'
            }
            rules={
              editingId == null
                ? [{ required: true, message: policyType === 'github' ? '请输入 GitHub Token' : policyType === 'terabox' ? '请输入 Client Secret' : policyType === 'baidu' ? '请输入 SecretKey' : policyType === 'filen' ? '请输入 Filen 密码' : policyType === 'dropbox' ? '请输入 Dropbox Access Token' : '请输入 Secret Key' }]
                : []
            }
            extra={editingId != null ? '留空表示不修改原密钥' : undefined}
          >
            <Input.Password
              placeholder={editingId != null ? '留空则不修改' : (policyType === 'github' ? 'GitHub Personal Access Token' : policyType === 'terabox' ? 'client_secret' : policyType === 'baidu' ? 'secret_key' : policyType === 'filen' ? 'Filen 账号密码' : policyType === 'dropbox' ? 'Dropbox Access Token（App Console 生成）' : 'Secret Access Key')}
              autoComplete="new-password"
            />
          </Form.Item>

          <Form.Item
            name="default_quota_gib"
            label="每用户默认配额 (GiB)"
            extra="仅作用于本存储策略。0 表示未配置/不可用，用户在该策略下无法上传。"
            rules={[
              {
                validator: async (_, v) => {
                  if (v === null || v === undefined || v === '') return
                  if (Number(v) < 0) throw new Error('不能为负数')
                },
              },
            ]}
          >
            <InputNumber min={0} step={1} style={{ width: '100%' }} placeholder="0" />
          </Form.Item>

          {policyType === 's3' && (
            <Form.Item
              name="chunk_size_mib"
              label="分片大小 (MiB)"
              extra="大文件分片上传时每片大小。0 表示默认 25 MiB；非 0 时最小 5 MiB（S3 协议要求），单文件最多 10000 片。"
              rules={[
                {
                  validator: async (_, v) => {
                    if (v === null || v === undefined || v === '') return
                    const n = Number(v)
                    if (n < 0) throw new Error('不能为负数')
                    if (n !== 0 && n < 5) throw new Error('非 0 时至少为 5 MiB')
                  },
                },
              ]}
            >
              <InputNumber min={0} step={1} style={{ width: '100%' }} placeholder="0（默认 25）" />
            </Form.Item>
          )}

          <Form.Item name="is_default" label="设为默认策略" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      {authTarget != null && authTarget.type === 'terabox' && (
        <TeraBoxAuth
          policyId={authTarget.id}
          open={authTarget != null}
          onClose={() => setAuthTarget(null)}
          onAuthorized={() => {
            setAuthTarget(null)
            load()
          }}
        />
      )}
      {authTarget != null && authTarget.type === 'baidu' && (
        <BaiduAuth
          policyId={authTarget.id}
          open={authTarget != null}
          onClose={() => setAuthTarget(null)}
          onAuthorized={() => {
            setAuthTarget(null)
            load()
          }}
        />
      )}
    </Layout>
  )
}
