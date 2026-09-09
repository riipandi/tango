import { Hr } from 'react-email'
import { Link, Button, CardFooter, CardHeader, Text } from '../components'
import { sharedPreviewProps, sharedTemplateProps } from '../constants'
import { BaseTemplate } from '../layouts'

interface OneTimeAccessData {
  name: string
  code: string
  loginLink: string
  buttonCodeLink: string
  expirationString: string
}

interface OneTimeAccessEmailProps {
  logoURL: string
  appName: string
  data: OneTimeAccessData
}

export const OneTimeAccessEmail = ({ logoURL, appName, data }: OneTimeAccessEmailProps) => {
  return (
    <BaseTemplate logoURL={logoURL} appName={appName}>
      <CardHeader title='Your Login Code' />
      <Hr style={{ marginTop: '16px' }} />

      <Text style={{ marginTop: '18px' }}>
        Click the button below to sign in to {appName} with a login code.
      </Text>

      <Button href={data.buttonCodeLink}>Sign In</Button>

      <Text style={{ marginTop: '20px' }}>
        Or visit: <Link href={data.loginLink}>{data.loginLink}</Link>
      </Text>

      <Text>
        then enter the one-time code: <strong>{data.code}</strong>
      </Text>

      <Hr style={{ marginTop: '24px' }} />

      <Text style={{ marginTop: '24px' }}>
        <strong>Important:</strong> This code will expire in {data.expirationString}.
      </Text>

      <Text>If you did not make this request, please ignore this email.</Text>

      <CardFooter>
        <Text size='sm' style={{ marginBottom: '4px' }}>
          You&apos;re receiving this email because you have an account in {appName}. <br />
          If you are not sure why you&apos;re receiving this, please contact us.
        </Text>
      </CardFooter>
    </BaseTemplate>
  )
}

OneTimeAccessEmail.TemplateProps = {
  ...sharedTemplateProps,
  data: {
    code: '{{.Data.Code}}',
    loginLink: '{{.Data.LoginLink}}',
    buttonCodeLink: '{{.Data.LoginLinkWithCode}}',
    expirationString: '{{.Data.ExpirationString}}'
  }
}

OneTimeAccessEmail.PreviewProps = {
  ...sharedPreviewProps,
  data: {
    code: '123456',
    loginLink: 'https://example.com/signin?method=onetimecode',
    buttonCodeLink: 'https://example.com/login?code=123456',
    expirationString: '15 minutes'
  }
}

export default OneTimeAccessEmail
