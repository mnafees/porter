import React from "react";
import Button from "components/porter/Button";
import styled from "styled-components";
import Input from "components/porter/Input";
import Spacer from "components/porter/Spacer";
import { useFieldArray, useFormContext } from "react-hook-form";
import { PorterAppFormData } from "lib/porter-apps";
import { ClientDomains } from "lib/porter-apps/values";
import { ControlledInput } from "components/porter/ControlledInput";

interface Props {
  index: number;
  customDomains: ClientDomains;
}

const CustomDomains: React.FC<Props> = ({ index, customDomains }) => {
  const { control, register } = useFormContext<PorterAppFormData>();
  const { remove, append, fields } = useFieldArray({
    control,
    name: `app.services.${index}.config.domains`,
  });

  return (
    <CustomDomainsContainer>
      {fields.length !== 0 && (
        <>
          {fields.map((customDomain, i) => {
            return (
              <div key={customDomain.id}>
                <AnnotationContainer>
                  <ControlledInput
                    type="text"
                    placeholder="ex: my-app.my-domain.com"
                    disabled={customDomain.name.readOnly}
                    width="275px"
                    disabledTooltip={
                      "You may only edit this field in your porter.yaml."
                    }
                    {...register(
                      `app.services.${index}.config.domains.${i}.name.value`
                    )}
                  />
                  <DeleteButton
                    onClick={() => {
                      //remove customDomain at the index
                      remove(i);
                    }}
                  >
                    <i className="material-icons">cancel</i>
                  </DeleteButton>
                </AnnotationContainer>
                <Spacer y={0.25} />
              </div>
            );
          })}
          <Spacer y={0.5} />
        </>
      )}
      <Button
        onClick={() => {
          append({
            name: {
              readOnly: false,
              value: "",
            },
          });
        }}
      >
        + Add Custom Domain
      </Button>
    </CustomDomainsContainer>
  );
};

export default CustomDomains;

const CustomDomainsContainer = styled.div``;

const AnnotationContainer = styled.div`
  display: flex;
  align-items: center;
  gap: 5px;
`;

const DeleteButton = styled.div`
  width: 15px;
  height: 15px;
  display: flex;
  align-items: center;
  margin-left: 8px;
  margin-top: -3px;
  justify-content: center;

  > i {
    font-size: 17px;
    color: #ffffff44;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    :hover {
      color: #ffffff88;
    }
  }
`;
