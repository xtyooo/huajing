/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, Loader2 } from 'lucide-react'
import { useEffect, useState, useCallback, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
import { JsonEditor } from '@/components/json-editor'
import { TagInput } from '@/components/tag-input'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  useSystemOptions,
  getOptionValue,
} from '@/features/system-settings/hooks/use-system-options'
import type { ModelSettings } from '@/features/system-settings/types'
import { safeJsonParse } from '@/features/system-settings/utils/json-parser'

import { saveModelWithPricing, getModel, getVendors } from '../../api'
import { getNameRuleOptions, ENDPOINT_TEMPLATES } from '../../constants'
import { modelsQueryKeys, vendorsQueryKeys, parseModelTags } from '../../lib'
import {
  buildModelPricingMutation,
  type ModelPricingMode,
} from '../../lib/model-pricing-options'
import type { Model } from '../../types'

// Extended schema for ratio configuration (internal form state only)
const extendedModelFormSchema = z.object({
  id: z.number().optional(),
  model_name: z.string().min(1, 'Model name is required'),
  description: z.string(),
  icon: z.string(),
  tags: z.array(z.string()),
  vendor_id: z.number().optional(),
  endpoints: z.string(),
  name_rule: z.number(),
  status: z.boolean(),
  sync_official: z.boolean(),
  price: z.string().optional(),
  ratio: z.string().optional(),
  cacheRatio: z.string().optional(),
  completionRatio: z.string().optional(),
  imageRatio: z.string().optional(),
  audioRatio: z.string().optional(),
  audioCompletionRatio: z.string().optional(),
})

type ExtendedModelFormValues = z.infer<typeof extendedModelFormSchema>

type PricingMode = ModelPricingMode
type PricingSubMode = 'ratio' | 'price'

type ModelMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Model | null
}

export function ModelMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: ModelMutateDrawerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const currentModelId = currentRow?.id
  const isEditing = Boolean(currentModelId)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [pricingMode, setPricingMode] = useState<PricingMode>('per-token')
  const [pricingSubMode, setPricingSubMode] = useState<PricingSubMode>('ratio')
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [promptPrice, setPromptPrice] = useState('')
  const [completionPrice, setCompletionPrice] = useState('')
  const [oldModelName, setOldModelName] = useState<string>('')
  const [resolution480p, setResolution480p] = useState('')
  const [resolution720p, setResolution720p] = useState('')
  const [resolution1080p, setResolution1080p] = useState('')
  const [imageSize1k, setImageSize1k] = useState('')
  const [imageSize2k, setImageSize2k] = useState('')
  const [imageSize4k, setImageSize4k] = useState('')
  const [skipSeconds, setSkipSeconds] = useState(false)
  const [tieredExpr, setTieredExpr] = useState('')

  // Fetch vendors for dropdown
  const { data: vendorsData } = useQuery({
    queryKey: vendorsQueryKeys.list(),
    queryFn: () => getVendors({ page_size: 1000 }),
    enabled: open,
  })

  const vendors = vendorsData?.data?.items || []

  // Fetch model detail if editing
  const { data: modelData } = useQuery({
    queryKey: modelsQueryKeys.detail(currentModelId || 0),
    queryFn: () => {
      if (!currentModelId) {
        throw new Error('Model ID is required')
      }
      return getModel(currentModelId)
    },
    enabled: open && isEditing,
  })

  // Fetch system options for ratio configuration
  const { data: systemOptionsData } = useSystemOptions()

  // Get model settings from system options
  const modelSettings = useMemo(() => {
    if (!systemOptionsData?.data) return null
    const defaultModelSettings: ModelSettings = {
      'global.pass_through_request_enabled': false,
      'global.thinking_model_blacklist': '[]',
      'global.chat_completions_to_responses_policy': '{}',
      'general_setting.ping_interval_enabled': false,
      'general_setting.ping_interval_seconds': 60,
      'gemini.safety_settings': '',
      'gemini.version_settings': '',
      'gemini.supported_imagine_models': '',
      'gemini.thinking_adapter_enabled': false,
      'gemini.thinking_adapter_budget_tokens_percentage': 0.6,
      'gemini.function_call_thought_signature_enabled': false,
      'gemini.remove_function_response_id_enabled': true,
      'claude.model_headers_settings': '',
      'claude.default_max_tokens': '',
      'claude.thinking_adapter_enabled': true,
      'claude.thinking_adapter_budget_tokens_percentage': 0.8,
      ModelPrice: '',
      ModelRatio: '',
      CacheRatio: '',
      CompletionRatio: '',
      ImageRatio: '',
      AudioRatio: '',
      AudioCompletionRatio: '',
      ExposeRatioEnabled: false,
      'billing_setting.billing_mode': '{}',
      'billing_setting.billing_expr': '{}',
      'billing_setting.skip_seconds': '{}',
      'tool_price_setting.prices': '{}',
      TopupGroupRatio: '',
      GroupRatio: '',
      UserUsableGroups: '',
      GroupGroupRatio: '',
      AutoGroups: '',
      // 官方新增了自动分组数量上限，模型编辑器读取系统设置时必须提供完整默认值。
      MaxTokenAutoGroups: 5,
      DefaultUseAutoGroup: false,
      CreateCacheRatio: '',
      'group_ratio_setting.group_special_usable_group': '{}',
      'grok.violation_deduction_enabled': false,
      'grok.violation_deduction_amount': 0,
      RetryTimes: 0,
      ChannelDisableThreshold: '',
      AutomaticDisableChannelEnabled: false,
      AutomaticEnableChannelEnabled: false,
      AutomaticDisableKeywords: '',
      AutomaticDisableStatusCodes: '401',
      AutomaticRetryStatusCodes:
        '100-199,300-399,401-407,409-499,500-503,505-523,525-599',
      'monitor_setting.auto_test_channel_enabled': false,
      'monitor_setting.auto_test_channel_minutes': 10,
      'monitor_setting.channel_test_mode': 'scheduled_all',
      'channel_affinity_setting.enabled': false,
      'channel_affinity_setting.switch_on_success': true,
      'channel_affinity_setting.keep_on_channel_disabled': false,
      'channel_affinity_setting.max_entries': 100000,
      'channel_affinity_setting.default_ttl_seconds': 3600,
      'channel_affinity_setting.rules': '[]',
      'model_deployment.ionet.api_key': '',
      'model_deployment.ionet.enabled': false,
    }
    return getOptionValue(systemOptionsData.data, defaultModelSettings)
  }, [systemOptionsData])

  const form = useForm<ExtendedModelFormValues>({
    resolver: zodResolver(extendedModelFormSchema),
    defaultValues: {
      model_name: '',
      description: '',
      icon: '',
      tags: [],
      vendor_id: undefined,
      endpoints: '',
      name_rule: 0,
      status: true,
      sync_official: true,
      price: '',
      ratio: '',
      cacheRatio: '',
      completionRatio: '',
      imageRatio: '',
      audioRatio: '',
      audioCompletionRatio: '',
    },
  })

  const validateNumber = (value: string) => {
    if (value === '') return true
    return Number.isFinite(Number(value))
  }

  const validateNonNegative = (value: string) => {
    if (value === '') return true
    const n = Number(value)
    return Number.isFinite(n) && n >= 0
  }

  const handlePromptPriceChange = (value: string) => {
    setPromptPrice(value)
    if (value && !Number.isNaN(Number.parseFloat(value))) {
      const ratio = Number.parseFloat(value) / 2
      form.setValue('ratio', ratio.toString())
    } else {
      form.setValue('ratio', '')
    }
  }

  const handleCompletionPriceChange = (value: string) => {
    setCompletionPrice(value)
    if (
      value &&
      !Number.isNaN(Number.parseFloat(value)) &&
      promptPrice &&
      !Number.isNaN(Number.parseFloat(promptPrice)) &&
      Number.parseFloat(promptPrice) > 0
    ) {
      const completionRatio =
        Number.parseFloat(value) / Number.parseFloat(promptPrice)
      form.setValue('completionRatio', completionRatio.toString())
    } else {
      form.setValue('completionRatio', '')
    }
  }

  // Load model data for editing and ratio configuration
  useEffect(() => {
    if (open && isEditing && modelData?.data) {
      const model = modelData.data
      setOldModelName(model.model_name)
      setPricingSubMode('ratio')
      setSkipSeconds(false)
      setPromptPrice('')
      setCompletionPrice('')
      setResolution480p('')
      setResolution720p('')
      setResolution1080p('')
      setImageSize1k('')
      setImageSize2k('')
      setImageSize4k('')
      setTieredExpr('')

      // Base model data reset
      const baseModelData = {
        id: model.id,
        model_name: model.model_name,
        description: model.description || '',
        icon: model.icon || '',
        tags: parseModelTags(model.tags),
        vendor_id: model.vendor_id,
        endpoints: model.endpoints || '',
        name_rule: model.name_rule || 0,
        status: model.status === 1,
        sync_official: model.sync_official === 1,
        price: '',
        ratio: '',
        cacheRatio: '',
        completionRatio: '',
        imageRatio: '',
        audioRatio: '',
        audioCompletionRatio: '',
      }

      // Parse ratio configurations from system settings if available
      if (modelSettings) {
        const priceMap = safeJsonParse<Record<string, number>>(
          modelSettings.ModelPrice,
          { fallback: {}, silent: true }
        )
        const ratioMap = safeJsonParse<Record<string, number>>(
          modelSettings.ModelRatio,
          { fallback: {}, silent: true }
        )
        const cacheMap = safeJsonParse<Record<string, number>>(
          modelSettings.CacheRatio,
          { fallback: {}, silent: true }
        )
        const completionMap = safeJsonParse<Record<string, number>>(
          modelSettings.CompletionRatio,
          { fallback: {}, silent: true }
        )
        const imageMap = safeJsonParse<Record<string, number>>(
          modelSettings.ImageRatio,
          { fallback: {}, silent: true }
        )
        const audioMap = safeJsonParse<Record<string, number>>(
          modelSettings.AudioRatio,
          { fallback: {}, silent: true }
        )
        const audioCompletionMap = safeJsonParse<Record<string, number>>(
          modelSettings.AudioCompletionRatio,
          { fallback: {}, silent: true }
        )

        // Extract ratio config for this model
        const modelName = model.model_name
        const price = priceMap[modelName]
        const ratio = ratioMap[modelName]
        const cacheRatio = cacheMap[modelName]
        const completionRatio = completionMap[modelName]
        const imageRatio = imageMap[modelName]
        const audioRatio = audioMap[modelName]
        const audioCompletionRatio = audioCompletionMap[modelName]
        const billingModeMap = safeJsonParse<Record<string, string>>(
          modelSettings['billing_setting.billing_mode'] || '{}',
          { fallback: {}, silent: true }
        )
        const billingExprMap = safeJsonParse<Record<string, string>>(
          modelSettings['billing_setting.billing_expr'] || '{}',
          { fallback: {}, silent: true }
        )
        const existingTieredExpr = billingExprMap[modelName] || ''

        const imageSizeKey = `image_size_price_setting.${modelName}`
        const imageSizeOption = systemOptionsData?.data?.find(
          (option) => option.key === imageSizeKey
        )
        const imageSizeSetting = imageSizeOption
          ? safeJsonParse<{
              enabled: boolean
              setting: Record<string, number>
            }>(imageSizeOption.value, { fallback: undefined, silent: true })
          : undefined

        // Check custom pricing modes first
        const resolutionKey = `resolution_price_setting.${modelName}`
        const resolutionOption = systemOptionsData?.data?.find(
          (option) => option.key === resolutionKey
        )
        const resolutionSetting = resolutionOption
          ? safeJsonParse<{
              enabled: boolean
              setting: Record<string, number>
            }>(resolutionOption.value, { fallback: undefined, silent: true })
          : undefined

        // Determine pricing mode
        if (
          billingModeMap[modelName] === 'tiered_expr' &&
          existingTieredExpr.trim() !== ''
        ) {
          setPricingMode('tiered_expr')
          setTieredExpr(existingTieredExpr)
          form.reset(baseModelData)
          setAdvancedOpen(false)
        } else if (imageSizeSetting?.enabled) {
          setPricingMode('image-size')
          setImageSize1k(imageSizeSetting.setting['1k']?.toString() || '')
          setImageSize2k(imageSizeSetting.setting['2k']?.toString() || '')
          setImageSize4k(imageSizeSetting.setting['4k']?.toString() || '')
          form.reset(baseModelData)
          setAdvancedOpen(false)
        } else if (resolutionSetting?.enabled) {
          setPricingMode('resolution')
          setResolution480p(resolutionSetting.setting['480p']?.toString() || '')
          setResolution720p(resolutionSetting.setting['720p']?.toString() || '')
          setResolution1080p(
            resolutionSetting.setting['1080p']?.toString() || ''
          )
          form.reset(baseModelData)
          setAdvancedOpen(false)
        } else if (price !== undefined && price !== null) {
          setPricingMode('per-request')
          const skipSecondsMap = safeJsonParse<Record<string, boolean>>(
            modelSettings?.['billing_setting.skip_seconds'] || '{}',
            { fallback: {}, silent: true }
          )
          setSkipSeconds(!!skipSecondsMap[model.model_name])
          form.reset({
            ...baseModelData,
            price: price.toString(),
          })
        } else {
          setPricingMode('per-token')
          if (ratio !== undefined && ratio !== null) {
            const tokenPrice = ratio * 2
            setPromptPrice(tokenPrice.toString())
            if (completionRatio !== undefined && completionRatio !== null) {
              const compPrice = tokenPrice * completionRatio
              setCompletionPrice(compPrice.toString())
            }
          }
          form.reset({
            ...baseModelData,
            ratio: ratio?.toString() || '',
            cacheRatio: cacheRatio?.toString() || '',
            completionRatio: completionRatio?.toString() || '',
            imageRatio: imageRatio?.toString() || '',
            audioRatio: audioRatio?.toString() || '',
            audioCompletionRatio: audioCompletionRatio?.toString() || '',
          })
          setAdvancedOpen(
            !!(cacheRatio || imageRatio || audioRatio || audioCompletionRatio)
          )
        }
      } else {
        // If system settings not loaded yet, just load base model data
        setPricingMode('per-token')
        setSkipSeconds(false)
        form.reset(baseModelData)
        setAdvancedOpen(false)
      }
    } else if (open && !isEditing) {
      // Pre-fill model name if passed from missing models
      setOldModelName('')
      setPricingMode('per-token')
      setSkipSeconds(false)
      setPricingSubMode('ratio')
      setPromptPrice('')
      setCompletionPrice('')
      setResolution480p('')
      setResolution720p('')
      setResolution1080p('')
      setImageSize1k('')
      setImageSize2k('')
      setImageSize4k('')
      setAdvancedOpen(false)
      form.reset({
        model_name: currentRow?.model_name || '',
        description: '',
        icon: '',
        tags: [],
        vendor_id: undefined,
        endpoints: '',
        name_rule: 0,
        status: true,
        sync_official: true,
        price: '',
        ratio: '',
        cacheRatio: '',
        completionRatio: '',
        imageRatio: '',
        audioRatio: '',
        audioCompletionRatio: '',
      })
    }
  }, [
    open,
    isEditing,
    modelData,
    currentRow,
    form,
    modelSettings,
    systemOptionsData?.data,
  ])

  const onSubmit = useCallback(
    async (values: ExtendedModelFormValues): Promise<void> => {
      setIsSubmitting(true)
      try {
        if (isEditing && !modelSettings) {
          throw new Error(t('Pricing settings are still loading'))
        }

        if (pricingMode === 'image-size') {
          const imageSizePrices = [imageSize1k, imageSize2k, imageSize4k]
          if (
            imageSizePrices.some(
              (price) =>
                price === '' ||
                !Number.isFinite(Number(price)) ||
                Number(price) <= 0
            )
          ) {
            throw new Error(
              t('Please configure prices greater than 0 for 1K, 2K, and 4K')
            )
          }
        }

        const submitData = {
          ...values,
          id: isEditing ? currentModelId : undefined,
          tags: Array.isArray(values.tags) ? values.tags.join(',') : '',
          status: values.status ? 1 : 0,
          sync_official: values.sync_official ? 1 : 0,
        }

        // Remove ratio fields from model data (they're stored in system settings)
        const {
          price,
          ratio,
          cacheRatio,
          completionRatio,
          imageRatio,
          audioRatio,
          audioCompletionRatio,
          ...modelData
        } = submitData

        const pricing = buildModelPricingMutation({
          mode: pricingMode,
          values: {
            price,
            ratio,
            cacheRatio,
            completionRatio,
            imageRatio,
            audioRatio,
            audioCompletionRatio,
          },
          skipSeconds,
          resolutionPrices: {
            '480p': resolution480p,
            '720p': resolution720p,
            '1080p': resolution1080p,
          },
          imageSizePrices: {
            '1k': imageSize1k,
            '2k': imageSize2k,
            '4k': imageSize4k,
          },
        })
        const response = await saveModelWithPricing({
          model: modelData,
          old_model_name: isEditing ? oldModelName : '',
          pricing,
        })

        if (!response.success) {
          toast.error(response.message || 'Operation failed')
          return
        }

        toast.success(
          isEditing
            ? 'Model updated successfully'
            : 'Model created successfully'
        )
        queryClient.invalidateQueries({ queryKey: modelsQueryKeys.lists() })
        queryClient.invalidateQueries({ queryKey: ['system-options'] })
        onOpenChange(false)
      } catch (error: unknown) {
        toast.error((error as Error)?.message || 'Operation failed')
      } finally {
        setIsSubmitting(false)
      }
    },
    [
      isEditing,
      currentModelId,
      queryClient,
      onOpenChange,
      pricingMode,
      oldModelName,
      skipSeconds,
      resolution480p,
      resolution720p,
      resolution1080p,
      imageSize1k,
      imageSize2k,
      imageSize4k,
      t,
      modelSettings,
    ]
  )

  const handleFillEndpointTemplate = (templateKey: string) => {
    const template = ENDPOINT_TEMPLATES[templateKey]
    if (template) {
      const templateJson = JSON.stringify({ [templateKey]: template }, null, 2)
      form.setValue('endpoints', templateJson)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isEditing ? t('Edit Model') : t('Create Model')}
          </SheetTitle>
          <SheetDescription>
            {isEditing
              ? t("Update model configuration and click save when you're done.")
              : t(
                  'Add a new model to the system by providing the necessary information.'
                )}
          </SheetDescription>
        </SheetHeader>

        <Form {...form}>
          <form
            id='model-form'
            onSubmit={form.handleSubmit(
              onSubmit as Parameters<typeof form.handleSubmit>[0]
            )}
            className={sideDrawerFormClassName()}
          >
            {/* Basic Information */}
            <SideDrawerSection>
              <h3 className='text-sm font-semibold'>
                {t('Basic Information')}
              </h3>

              <FormField
                control={form.control}
                name='model_name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Model Name *')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('gpt-4, claude-3-opus, etc.')}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('The unique identifier for this model')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='description'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Description')}</FormLabel>
                    <FormControl>
                      <Textarea
                        placeholder={t('Describe this model...')}
                        rows={3}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='icon'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Icon')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('OpenAI, Anthropic, etc.')}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription className='text-xs'>
                      {t('@lobehub/icons key')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='vendor_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Vendor')}</FormLabel>
                    <Select
                      items={vendors.map((vendor) => ({
                        value: String(vendor.id),
                        label: vendor.name,
                      }))}
                      onValueChange={(value) =>
                        field.onChange(
                          value ? Number.parseInt(value) : undefined
                        )
                      }
                      value={field.value ? String(field.value) : undefined}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder={t('Select vendor')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {vendors.map((vendor) => (
                            <SelectItem
                              key={vendor.id}
                              value={String(vendor.id)}
                            >
                              {vendor.name}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='tags'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Tags')}</FormLabel>
                    <FormControl>
                      <TagInput
                        value={field.value || []}
                        onChange={field.onChange}
                        placeholder={t('Add tags...')}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Press Enter or comma to add tags')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>

            {/* Matching Configuration */}
            <SideDrawerSection>
              <h3 className='text-sm font-semibold'>{t('Matching Rules')}</h3>

              <FormField
                control={form.control}
                name='name_rule'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Name Rule')}</FormLabel>
                    <FormControl>
                      <RadioGroup
                        onValueChange={(value) =>
                          field.onChange(Number.parseInt(value))
                        }
                        value={String(field.value)}
                        className='grid grid-cols-2 gap-4'
                      >
                        {getNameRuleOptions(t).map((option) => (
                          <div
                            key={option.value}
                            className='flex items-center space-x-2'
                          >
                            <RadioGroupItem
                              value={String(option.value)}
                              id={`rule-${option.value}`}
                            />
                            <Label
                              htmlFor={`rule-${option.value}`}
                              className='cursor-pointer font-normal'
                            >
                              {option.label}
                            </Label>
                          </div>
                        ))}
                      </RadioGroup>
                    </FormControl>
                    <FormDescription>
                      {t('How this model name should match requests')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>

            {/* Endpoints Configuration */}
            <SideDrawerSection>
              <div className='flex items-center justify-between'>
                <h3 className='text-sm font-semibold'>{t('Endpoints')}</h3>
                <Select<string>
                  items={Object.keys(ENDPOINT_TEMPLATES).map((key) => ({
                    value: key,
                    label: key,
                  }))}
                  onValueChange={(v) =>
                    v !== null && handleFillEndpointTemplate(v)
                  }
                >
                  <SelectTrigger size='sm' className='w-[200px]'>
                    <SelectValue placeholder={t('Load template...')} />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {Object.keys(ENDPOINT_TEMPLATES).map((key) => (
                        <SelectItem key={key} value={key}>
                          {key}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>

              <FormField
                control={form.control}
                name='endpoints'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Endpoint Configuration')}</FormLabel>
                    <FormControl>
                      <JsonEditor
                        value={field.value || ''}
                        onChange={field.onChange}
                        keyPlaceholder='endpoint_type'
                        valuePlaceholder='{"path": "/v1/...", "method": "POST"}'
                        keyLabel='Endpoint Type'
                        valueLabel='Configuration'
                        valueType='any'
                        emptyMessage={t(
                          'No endpoints configured. Switch to JSON mode or add rows to define endpoints.'
                        )}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Define API endpoints for this model (JSON format)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>

            {/* Pricing Configuration */}
            <SideDrawerSection>
              <h3 className='text-sm font-semibold'>
                {t('Pricing Configuration')}
              </h3>

              <div className='space-y-4'>
                <Label>{t('Pricing mode')}</Label>
                <RadioGroup
                  value={pricingMode}
                  onValueChange={(value) => {
                    const nextMode = value as PricingMode
                    if (nextMode !== 'per-request') {
                      setSkipSeconds(false)
                    }
                    setPricingMode(nextMode)
                  }}
                >
                  <div className='flex items-center space-x-2'>
                    <RadioGroupItem value='per-token' id='per-token' />
                    <Label htmlFor='per-token' className='font-normal'>
                      {t('Per-token (ratio based)')}
                    </Label>
                  </div>
                  <div className='flex items-center space-x-2'>
                    <RadioGroupItem value='per-request' id='per-request' />
                    <Label htmlFor='per-request' className='font-normal'>
                      {t('Per-request (fixed price)')}
                    </Label>
                  </div>
                  <div className='flex items-center space-x-2'>
                    <RadioGroupItem value='resolution' id='mode-resolution' />
                    <Label htmlFor='mode-resolution' className='font-normal'>
                      {t('Resolution (per second)')}
                    </Label>
                  </div>
                  <div className='flex items-center space-x-2'>
                    <RadioGroupItem value='image-size' id='mode-image-size' />
                    <Label htmlFor='mode-image-size' className='font-normal'>
                      {t('Image size (per image)')}
                    </Label>
                  </div>
                  <div className='flex items-center space-x-2'>
                    <RadioGroupItem
                      value='tiered_expr'
                      id='mode-tiered-expr'
                      disabled={!tieredExpr}
                    />
                    <Label htmlFor='mode-tiered-expr' className='font-normal'>
                      {t('Expression billing')}
                    </Label>
                  </div>
                </RadioGroup>
              </div>

              {pricingMode === 'tiered_expr' && (
                <Textarea
                  value={tieredExpr}
                  readOnly
                  className='font-mono text-xs'
                />
              )}

              {pricingMode === 'per-request' && (
                <>
                  <FormField
                    control={form.control}
                    name='price'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Fixed price (USD)')}</FormLabel>
                        <FormControl>
                          <Input
                            type='text'
                            placeholder='0.01'
                            {...field}
                            onChange={(e) => {
                              const value = e.target.value
                              if (validateNumber(value)) {
                                field.onChange(value)
                              }
                            }}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Cost in USD per request, regardless of tokens used.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <div className={sideDrawerSwitchItemClassName()}>
                    <div className='space-y-0.5'>
                      <Label className='text-sm font-medium'>
                        跳过秒数计算
                      </Label>
                    </div>
                    <Switch
                      checked={skipSeconds}
                      onCheckedChange={setSkipSeconds}
                    />
                  </div>
                </>
              )}
              {pricingMode === 'resolution' && (
                <div className='space-y-3'>
                  <p className='text-muted-foreground text-xs'>
                    {t('Price per second (USD)')}
                  </p>
                  <div className='flex items-center space-x-3'>
                    <Label className='w-12 text-xs'>480p</Label>
                    <Input
                      type='text'
                      placeholder='0'
                      value={resolution480p}
                      onChange={(e) => {
                        const v = e.target.value
                        if (validateNonNegative(v)) setResolution480p(v)
                      }}
                      className='w-24'
                    />
                    <Label className='w-12 text-xs'>720p</Label>
                    <Input
                      type='text'
                      placeholder='0'
                      value={resolution720p}
                      onChange={(e) => {
                        const v = e.target.value
                        if (validateNonNegative(v)) setResolution720p(v)
                      }}
                      className='w-24'
                    />
                    <Label className='w-12 text-xs'>1080p</Label>
                    <Input
                      type='text'
                      placeholder='0'
                      value={resolution1080p}
                      onChange={(e) => {
                        const v = e.target.value
                        if (validateNonNegative(v)) setResolution1080p(v)
                      }}
                      className='w-24'
                    />
                  </div>
                </div>
              )}
              {pricingMode === 'image-size' && (
                <div className='space-y-3'>
                  <div className='grid grid-cols-3 gap-3'>
                    {[
                      { label: '1K', value: imageSize1k, set: setImageSize1k },
                      { label: '2K', value: imageSize2k, set: setImageSize2k },
                      { label: '4K', value: imageSize4k, set: setImageSize4k },
                    ].map((item) => (
                      <div key={item.label} className='space-y-1.5'>
                        <Label className='text-xs'>{item.label}</Label>
                        <Input
                          type='text'
                          inputMode='decimal'
                          placeholder='0.01'
                          value={item.value}
                          onChange={(event) => {
                            const value = event.target.value
                            if (validateNonNegative(value)) item.set(value)
                          }}
                        />
                      </div>
                    ))}
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Price per generated image in USD. Requests without a size use the 1K price.'
                    )}
                  </p>
                </div>
              )}
              {pricingMode === 'per-token' && (
                <>
                  <div className='space-y-4'>
                    <Label>{t('Input mode')}</Label>
                    <RadioGroup
                      value={pricingSubMode}
                      onValueChange={(value) =>
                        setPricingSubMode(value as PricingSubMode)
                      }
                    >
                      <div className='flex items-center space-x-2'>
                        <RadioGroupItem value='ratio' id='ratio' />
                        <Label htmlFor='ratio' className='font-normal'>
                          {t('Ratio mode')}
                        </Label>
                      </div>
                      <div className='flex items-center space-x-2'>
                        <RadioGroupItem value='price' id='price' />
                        <Label htmlFor='price' className='font-normal'>
                          {t('Price mode (USD per 1M tokens)')}
                        </Label>
                      </div>
                    </RadioGroup>
                  </div>

                  {pricingSubMode === 'ratio' ? (
                    <>
                      <FormField
                        control={form.control}
                        name='ratio'
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Model ratio')}</FormLabel>
                            <FormControl>
                              <Input
                                type='text'
                                placeholder='1.0'
                                {...field}
                                onChange={(e) => {
                                  const value = e.target.value
                                  if (validateNumber(value)) {
                                    field.onChange(value)
                                    if (value) {
                                      setPromptPrice(
                                        (
                                          Number.parseFloat(value) * 2
                                        ).toString()
                                      )
                                    } else {
                                      setPromptPrice('')
                                    }
                                  }
                                }}
                              />
                            </FormControl>
                            <FormDescription>
                              {field.value &&
                              !Number.isNaN(Number.parseFloat(field.value))
                                ? `Calculated price: $${(Number.parseFloat(field.value) * 2).toFixed(4)} per 1M tokens`
                                : t('Multiplier for prompt tokens.')}
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />

                      <FormField
                        control={form.control}
                        name='completionRatio'
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Completion ratio')}</FormLabel>
                            <FormControl>
                              <Input
                                type='text'
                                placeholder='1.0'
                                {...field}
                                onChange={(e) => {
                                  const value = e.target.value
                                  if (validateNumber(value)) {
                                    field.onChange(value)
                                    const ratio = form.getValues('ratio')
                                    if (value && ratio) {
                                      const compPrice =
                                        Number.parseFloat(ratio) *
                                        2 *
                                        Number.parseFloat(value)
                                      setCompletionPrice(compPrice.toString())
                                    } else {
                                      setCompletionPrice('')
                                    }
                                  }
                                }}
                              />
                            </FormControl>
                            <FormDescription>
                              {field.value &&
                              !Number.isNaN(Number.parseFloat(field.value)) &&
                              promptPrice &&
                              !Number.isNaN(Number.parseFloat(promptPrice))
                                ? `Calculated price: $${(Number.parseFloat(promptPrice) * Number.parseFloat(field.value)).toFixed(4)} per 1M tokens`
                                : t('Multiplier for completion tokens.')}
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </>
                  ) : (
                    <div className='space-y-4'>
                      <div className='space-y-2'>
                        <Label>{t('Prompt price ($/1M tokens)')}</Label>
                        <Input
                          type='text'
                          placeholder='2.0'
                          value={promptPrice}
                          onChange={(e) =>
                            handlePromptPriceChange(e.target.value)
                          }
                        />
                        <p className='text-muted-foreground text-sm'>
                          {promptPrice &&
                          !Number.isNaN(Number.parseFloat(promptPrice))
                            ? `Calculated ratio: ${(Number.parseFloat(promptPrice) / 2).toFixed(4)}`
                            : t('Enter Input price to calculate ratio')}
                        </p>
                      </div>

                      <div className='space-y-2'>
                        <Label>{t('Completion price ($/1M tokens)')}</Label>
                        <Input
                          type='text'
                          placeholder='4.0'
                          value={completionPrice}
                          onChange={(e) =>
                            handleCompletionPriceChange(e.target.value)
                          }
                        />
                        <p className='text-muted-foreground text-sm'>
                          {completionPrice &&
                          !Number.isNaN(Number.parseFloat(completionPrice)) &&
                          promptPrice &&
                          !Number.isNaN(Number.parseFloat(promptPrice)) &&
                          Number.parseFloat(promptPrice) > 0
                            ? `Calculated ratio: ${(Number.parseFloat(completionPrice) / Number.parseFloat(promptPrice)).toFixed(4)}`
                            : t('Enter Completion price to calculate ratio')}
                        </p>
                      </div>
                    </div>
                  )}

                  <Collapsible
                    open={advancedOpen}
                    onOpenChange={setAdvancedOpen}
                  >
                    <CollapsibleTrigger
                      render={
                        <Button
                          type='button'
                          variant='outline'
                          className='flex w-full items-center justify-between'
                        />
                      }
                    >
                      {t('Advanced options')}
                      <ChevronDown
                        className={`h-4 w-4 transition-transform duration-200 ${
                          advancedOpen ? 'rotate-180' : ''
                        }`}
                      />
                    </CollapsibleTrigger>
                    <CollapsibleContent className='flex flex-col gap-4 pt-4'>
                      <FormField
                        control={form.control}
                        name='cacheRatio'
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Cache ratio')}</FormLabel>
                            <FormControl>
                              <Input
                                type='text'
                                placeholder='0.1'
                                {...field}
                                onChange={(e) => {
                                  const value = e.target.value
                                  if (validateNumber(value)) {
                                    field.onChange(value)
                                  }
                                }}
                              />
                            </FormControl>
                            <FormDescription>
                              {t('Discount ratio for cache hits.')}
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />

                      <FormField
                        control={form.control}
                        name='imageRatio'
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Image ratio')}</FormLabel>
                            <FormControl>
                              <Input
                                type='text'
                                placeholder='1.0'
                                {...field}
                                onChange={(e) => {
                                  const value = e.target.value
                                  if (validateNumber(value)) {
                                    field.onChange(value)
                                  }
                                }}
                              />
                            </FormControl>
                            <FormDescription>
                              {t('Multiplier for image processing.')}
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />

                      <FormField
                        control={form.control}
                        name='audioRatio'
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Audio ratio')}</FormLabel>
                            <FormControl>
                              <Input
                                type='text'
                                placeholder='1.0'
                                {...field}
                                onChange={(e) => {
                                  const value = e.target.value
                                  if (validateNumber(value)) {
                                    field.onChange(value)
                                  }
                                }}
                              />
                            </FormControl>
                            <FormDescription>
                              {t('Multiplier for audio inputs.')}
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />

                      <FormField
                        control={form.control}
                        name='audioCompletionRatio'
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Audio completion ratio')}</FormLabel>
                            <FormControl>
                              <Input
                                type='text'
                                placeholder='1.0'
                                {...field}
                                onChange={(e) => {
                                  const value = e.target.value
                                  if (validateNumber(value)) {
                                    field.onChange(value)
                                  }
                                }}
                              />
                            </FormControl>
                            <FormDescription>
                              {t('Multiplier for audio outputs.')}
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </CollapsibleContent>
                  </Collapsible>
                </>
              )}
            </SideDrawerSection>

            {/* Status & Sync */}
            <SideDrawerSection>
              <h3 className='text-sm font-semibold'>{t('Status & Sync')}</h3>

              <FormField
                control={form.control}
                name='status'
                render={({ field }) => (
                  <FormItem className={sideDrawerSwitchItemClassName()}>
                    <div className='flex flex-col gap-0.5'>
                      <FormLabel className='text-base'>
                        {t('Enabled')}
                      </FormLabel>
                      <FormDescription>
                        {t('Enable or disable this model')}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='sync_official'
                render={({ field }) => (
                  <FormItem className={sideDrawerSwitchItemClassName()}>
                    <div className='flex flex-col gap-0.5'>
                      <FormLabel className='text-base'>
                        {t('Official Sync')}
                      </FormLabel>
                      <FormDescription>
                        {t('Sync this model with official upstream')}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </SideDrawerSection>
          </form>
        </Form>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose
            render={<Button variant='outline' disabled={isSubmitting} />}
          >
            {t('Cancel')}
          </SheetClose>
          <Button
            form='model-form'
            type='submit'
            disabled={isSubmitting || (isEditing && !modelSettings)}
          >
            {isSubmitting && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {isEditing ? t('Update Model') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
