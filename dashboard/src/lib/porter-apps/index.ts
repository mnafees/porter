import { buildpackSchema } from "main/home/app-dashboard/types/buildpack";
import { z } from "zod";
import {
  DetectedServices,
  defaultSerialized,
  deserializeService,
  isPredeployService,
  serializeService,
  serializedServiceFromProto,
  serviceProto,
  serviceValidator,
} from "./services";
import { Build, PorterApp, Service } from "@porter-dev/api-contracts";
import { match } from "ts-pattern";
import { valueExists } from "shared/util";

// buildValidator is used to validate inputs for build setting fields
export const buildValidator = z.discriminatedUnion("method", [
  z.object({
    method: z.literal("pack"),
    context: z.string().default("./"),
    buildpacks: z.array(buildpackSchema).default([]),
    builder: z.string(),
  }),
  z.object({
    method: z.literal("docker"),
    context: z.string().default("./"),
    dockerfile: z.string().default("./Dockerfile"),
  }),
]);
export type BuildOptions = z.infer<typeof buildValidator>;

// sourceValidator is used to validate inputs for source setting fields
export const sourceValidator = z.discriminatedUnion("type", [
  z.object({
    type: z.literal("github"),
    git_repo_id: z.number().min(1),
    git_branch: z.string().min(1),
    git_repo_name: z.string().min(1),
    porter_yaml_path: z.string().default("./porter.yaml"),
  }),
  z.object({
    type: z.literal("docker-registry"),
    // add branch and repo as undefined to allow for easy checks on changes to the source type
    // (i.e. we want to remove the services if any source fields change)
    git_branch: z.undefined(),
    git_repo_name: z.undefined(),
    image: z.object({
      repository: z.string().min(1),
      tag: z.string().default("latest"),
    }),
  }),
]);
export type SourceOptions = z.infer<typeof sourceValidator>;

// clientAppValidator is the representation of a Porter app on the client, and is used to validate inputs for app setting fields
export const clientAppValidator = z.object({
  name: z.string().min(1),
  services: serviceValidator.array(),
  env: z.record(z.string(), z.string()).default({}),
  build: buildValidator,
});
export type ClientPorterApp = z.infer<typeof clientAppValidator>;

// porterAppFormValidator is used to validate inputs when creating + updating an app
export const porterAppFormValidator = z.object({
  app: clientAppValidator,
  source: sourceValidator,
});
export type PorterAppFormData = z.infer<typeof porterAppFormValidator>;

// serviceOverrides is used to generate the services overrides for an app from porter.yaml
// this method is only called when a porter.yaml is present and has services defined
export function serviceOverrides({
  overrides,
  useDefaults = true,
}: {
  overrides: PorterApp;
  useDefaults?: boolean;
}): DetectedServices {
  const services = Object.entries(overrides.services)
    .map(([name, service]) => serializedServiceFromProto({ name, service }))
    .map((svc) => {
      if (useDefaults) {
        return deserializeService({
          service: defaultSerialized({ name: svc.name, type: svc.config.type }),
          override: svc,
          expanded: true,
        });
      }

      return deserializeService({ service: svc });
    });

  if (!overrides.predeploy) {
    return {
      services,
    };
  }

  if (useDefaults) {
    return {
      services,
      predeploy: deserializeService({
        service: defaultSerialized({
          name: "pre-deploy",
          type: "predeploy",
        }),
        override: serializedServiceFromProto({
          name: "pre-deploy",
          service: overrides.predeploy,
          isPredeploy: true,
        }),
        expanded: true,
      }),
    };
  }

  return {
    services,
    predeploy: deserializeService({
      service: serializedServiceFromProto({
        name: "pre-deploy",
        service: overrides.predeploy,
        isPredeploy: true,
      }),
    }),
  };
}

const clientBuildToProto = (build: BuildOptions) => {
  return match(build)
    .with({ method: "pack" }, (b) =>
      Object.freeze({
        method: "pack",
        context: b.context,
        buildpacks: b.buildpacks.map((b) => b.buildpack),
        builder: b.builder,
      })
    )
    .with({ method: "docker" }, (b) =>
      Object.freeze({
        method: "docker",
        context: b.context,
        dockerfile: b.dockerfile,
      })
    )
    .exhaustive();
};

export function clientAppToProto(data: PorterAppFormData): PorterApp {
  const { app, source } = data;

  const services = app.services
    .filter((s) => !isPredeployService(s))
    .reduce((acc: Record<string, Service>, svc) => {
      acc[svc.name.value] = serviceProto(serializeService(svc));
      return acc;
    }, {});

  const predeploy = app.services.find((s) => isPredeployService(s));

  const proto = match(source)
    .with(
      { type: "github" },
      () =>
        new PorterApp({
          name: app.name,
          services,
          env: app.env,
          build: clientBuildToProto(app.build),
          ...(predeploy && {
            predeploy: serviceProto(serializeService(predeploy)),
          }),
        })
    )
    .with(
      { type: "docker-registry" },
      (src) =>
        new PorterApp({
          name: app.name,
          services,
          env: app.env,
          image: {
            repository: src.image.repository,
            tag: src.image.tag,
          },
        })
    )
    .exhaustive();

  return proto;
}

const clientBuildFromProto = (proto?: Build): BuildOptions | undefined => {
  if (!proto) {
    return;
  }

  const buildValidation = z
    .discriminatedUnion("method", [
      z.object({
        method: z.literal("pack"),
        context: z.string(),
        buildpacks: z.array(z.string()).default([]),
        builder: z.string(),
      }),
      z.object({
        method: z.literal("docker"),
        context: z.string(),
        dockerfile: z.string(),
      }),
    ])
    .safeParse(proto);

  if (!buildValidation.success) {
    return;
  }

  const build = buildValidation.data;

  return match(build)
    .with({ method: "pack" }, (b) =>
      Object.freeze({
        method: b.method,
        context: b.context,
        buildpacks: b.buildpacks.map((b) => ({ name: b, buildpack: b })),
        builder: b.builder,
      })
    )
    .with({ method: "docker" }, (b) =>
      Object.freeze({
        method: b.method,
        context: b.context,
        dockerfile: b.dockerfile,
      })
    )
    .exhaustive();
};

export function clientAppFromProto(
  proto: PorterApp,
  overrides: DetectedServices | null
): ClientPorterApp {
  const services = Object.entries(proto.services)
    .map(([name, service]) => serializedServiceFromProto({ name, service }))
    .map((svc) => {
      const override = overrides?.services.find(
        (s) => s.name.value === svc.name
      );

      if (override) {
        return deserializeService({
          service: svc,
          override: serializeService(override),
        });
      }
      return deserializeService({ service: svc });
    });

  if (!overrides?.predeploy) {
    return {
      name: proto.name,
      services,
      env: proto.env,
      build: clientBuildFromProto(proto.build) ?? {
        method: "pack",
        context: "./",
        buildpacks: [],
        builder: "",
      },
    };
  }

  const predeployOverrides = serializeService(overrides.predeploy);
  const predeploy = proto.predeploy
    ? deserializeService({
        service: serializedServiceFromProto({
          name: "pre-deploy",
          service: proto.predeploy,
          isPredeploy: true,
        }),
        override: predeployOverrides,
      })
    : undefined;

  return {
    name: proto.name,
    services: [...services, predeploy].filter(valueExists),
    env: proto.env,
    build: clientBuildFromProto(proto.build) ?? {
      method: "pack",
      context: "./",
      buildpacks: [],
      builder: "",
    },
  };
}
