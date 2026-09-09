import type { CSSProperties } from 'react'
import { Hr, CodeInline, Row, Column } from 'react-email'
import { Alert, Button, CardFooter, CardHeader, Text } from '../components'
import { sharedPreviewProps, sharedTemplateProps } from '../constants'
import { BaseTemplate } from '../layouts'

interface EmailChangeRequestData {
  name: string
  oldEmail: string
  newEmail: string
  confirmLink: string
}

interface EmailChangeRequestProps {
  logoURL: string
  appName: string
  data: EmailChangeRequestData
}

export const EmailChangeRequest = ({ logoURL, appName, data }: EmailChangeRequestProps) => {
  const detailsBoxStyle = { width: '225px', padding: '12px' } satisfies CSSProperties
  const detailsLabelStyle = { margin: 0, color: '#747474' } satisfies CSSProperties
  const detailsBoxValueStyle = { margin: 0, fontSize: '14px' } satisfies CSSProperties

  return (
    <BaseTemplate logoURL={logoURL} appName={appName}>
      <CardHeader title='Email Change Request' />
      <Hr style={{ marginTop: '16px' }} />
      <Text style={{ marginTop: '18px' }}>Hello {data.name},</Text>

      <Text>You requested to change your email address:</Text>

      <Row style={{ backgroundColor: '#f6f6f6', borderRadius: '12px' }}>
        <Column style={detailsBoxStyle}>
          <Text size='sm' style={detailsLabelStyle}>
            Current Email
          </Text>
          <Text style={detailsBoxValueStyle}>{data.oldEmail}</Text>
        </Column>
        <Column style={detailsBoxStyle}>
          <Text size='sm' style={detailsLabelStyle}>
            New Email
          </Text>
          <Text style={detailsBoxValueStyle}>{data.newEmail}</Text>
        </Column>
      </Row>

      <Hr style={{ marginTop: '20px' }} />

      <Text>Please click the link below to confirm this change:</Text>

      <Button href={data.confirmLink}>Confirm Email Change</Button>

      <Text style={{ marginTop: '24px', marginBottom: '8px' }}>
        Or copy and paste this link into your browser:
      </Text>

      <CodeInline style={{ color: '#5b667e', fontSize: '14px', wordBreak: 'break-all' }}>
        {data.confirmLink}
      </CodeInline>

      <Hr style={{ marginTop: '20px' }} />

      <Alert variant='warning' style={{ marginTop: '20px' }}>
        <strong>Important:</strong> This link will expire in 24 hours. If you did not request this
        change, please ignore this email.
      </Alert>

      <CardFooter>
        <Text size='sm' style={{ marginBottom: '4px' }}>
          You&apos;re receiving this email because you have an account in {appName}. <br />
          If you are not sure why you&apos;re receiving this, please contact us.
        </Text>
      </CardFooter>
    </BaseTemplate>
  )
}

EmailChangeRequest.TemplateProps = {
  ...sharedTemplateProps,
  data: {
    name: '{{.Data.Name}}',
    oldEmail: '{{.Data.OldEmail}}',
    newEmail: '{{.Data.NewEmail}}',
    confirmLink: '{{.Data.ConfirmLink}}'
  }
}

EmailChangeRequest.PreviewProps = {
  ...sharedPreviewProps,
  data: {
    name: 'John Doe',
    oldEmail: 'current@example.com',
    newEmail: 'new@example.com',
    confirmLink: 'https://example.com/confirm-email-change?token=abc123'
  }
}

export default EmailChangeRequest
